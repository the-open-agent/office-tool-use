// Copyright 2026 The OpenAgent Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0

package smartart

import (
	"crypto/rand"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/the-open-agent/office-tool-use/model"
	"github.com/the-open-agent/office-tool-use/ooxml"
)

func removeSmartArtDrawingCache(pkg *ooxml.Package, rels *ooxml.Relationships, types *ooxml.ContentTypes, slidePart string, dataRoot *ooxml.Node) error {
	ext := dataRoot.FirstDescendant(ooxml.NSDiagram2008, "dataModelExt")
	if ext == nil || ext.AttrValue("", "relId") == "" {
		return nil
	}
	relID := ext.AttrValue("", "relId")
	drawingPart, err := relatedPartByID(slidePart, rels, relID, ooxml.RelationshipTypeDiagramDrawing)
	if err != nil {
		return err
	}
	rels.Remove(relID)
	if err := pkg.SetRelationships(slidePart, rels); err != nil {
		return err
	}
	if pkg.HasPart(drawingPart) {
		if err := pkg.DeletePart(drawingPart); err != nil {
			return err
		}
	}
	if types != nil {
		types.RemoveOverride(drawingPart)
	}
	smartArtRemoveDescendants(dataRoot, ooxml.NSDiagram2008, "dataModelExt")
	return nil
}

func smartArtRenumberResizeModel(model *smartArtResizeModel) {
	presOrder := 0
	for index := range model.Nodes {
		node := &model.Nodes[index]
		node.NormalCxn.SetAttr("", "srcOrd", strconv.Itoa(index))
		if prSet := node.PresNodePt.Child(ooxml.NSDiagram, "prSet"); prSet != nil {
			prSet.SetAttr("", "presStyleIdx", strconv.Itoa(index))
			prSet.SetAttr("", "presStyleCnt", strconv.Itoa(len(model.Nodes)))
		}
		node.NodePresParOf.SetAttr("", "srcOrd", strconv.Itoa(presOrder))
		presOrder++
		if node.SibTransPresParOf != nil && index < len(model.Nodes)-1 {
			node.SibTransPresParOf.SetAttr("", "srcOrd", strconv.Itoa(presOrder))
			presOrder++
		}
	}
}

func smartArtRenumberGenericResizeModel(model *smartArtResizeModel) {
	if model.Mode == "top_level_tail" {
		smartArtRenumberResizeModel(model)
		return
	}
	for groupIndex, group := range model.Groups {
		if group.RootCxn != nil {
			group.RootCxn.SetAttr("", "srcOrd", strconv.Itoa(groupIndex))
		}
	}
	for _, cxns := range smartArtCxnGroupsBySrc(model.CxnList, "") {
		sort.SliceStable(cxns, func(i, j int) bool {
			return smartArtIntAttr(cxns[i], "srcOrd") < smartArtIntAttr(cxns[j], "srcOrd")
		})
		for index, cxn := range cxns {
			cxn.SetAttr("", "srcOrd", strconv.Itoa(index))
		}
	}
	contentOrder := map[string]int{}
	for index, node := range model.Nodes {
		contentOrder[node.ContentID] = index
	}
	type presBucket struct {
		items []*ooxml.Node
	}
	buckets := map[string]*presBucket{}
	for _, pt := range model.PtList.NamedChildren(ooxml.NSDiagram, "pt") {
		if pt.AttrValue("", "type") != "pres" {
			continue
		}
		prSet := pt.Child(ooxml.NSDiagram, "prSet")
		if prSet == nil || prSet.AttrValue("", "presStyleIdx") == "" || prSet.AttrValue("", "presStyleCnt") == "" {
			continue
		}
		assocID := prSet.AttrValue("", "presAssocID")
		if _, ok := contentOrder[assocID]; !ok {
			continue
		}
		key := prSet.AttrValue("", "presName")
		if key == "" {
			key = prSet.AttrValue("", "presStyleLbl")
		}
		if key == "" {
			key = "pres"
		}
		if buckets[key] == nil {
			buckets[key] = &presBucket{}
		}
		buckets[key].items = append(buckets[key].items, pt)
	}
	for _, bucket := range buckets {
		sort.SliceStable(bucket.items, func(i, j int) bool {
			left := bucket.items[i].Child(ooxml.NSDiagram, "prSet").AttrValue("", "presAssocID")
			right := bucket.items[j].Child(ooxml.NSDiagram, "prSet").AttrValue("", "presAssocID")
			return contentOrder[left] < contentOrder[right]
		})
		for index, pt := range bucket.items {
			prSet := pt.Child(ooxml.NSDiagram, "prSet")
			prSet.SetAttr("", "presStyleIdx", strconv.Itoa(index))
			prSet.SetAttr("", "presStyleCnt", strconv.Itoa(len(bucket.items)))
		}
	}
	smartArtRenumberPresParOfByPhysicalOrder(model.CxnList)
}

func smartArtMoveNewestRootCxnBeforeFirstRootCxn(model *smartArtResizeModel) {
	var newest, first *ooxml.Node
	newestOrd := -1
	for _, cxn := range model.CxnList.NamedChildren(ooxml.NSDiagram, "cxn") {
		if cxn.AttrValue("", "type") != "" || cxn.AttrValue("", "srcId") != model.DocID {
			continue
		}
		ord := smartArtIntAttr(cxn, "srcOrd")
		if ord == 0 {
			first = cxn
		}
		if ord > newestOrd {
			newestOrd = ord
			newest = cxn
		}
	}
	if newest == nil || first == nil || newest == first {
		return
	}
	model.CxnList.Children = smartArtMoveChildBefore(model.CxnList.Children, newest, first)
}

func smartArtRenumberPresParOfByPhysicalOrder(cxnList *ooxml.Node) {
	nextOrdBySrc := map[string]int{}
	for _, cxn := range cxnList.NamedChildren(ooxml.NSDiagram, "cxn") {
		if cxn.AttrValue("", "type") != "presParOf" {
			continue
		}
		srcID := cxn.AttrValue("", "srcId")
		cxn.SetAttr("", "srcOrd", strconv.Itoa(nextOrdBySrc[srcID]))
		nextOrdBySrc[srcID]++
	}
}

func smartArtMoveChildBefore(children []*ooxml.Node, moving, before *ooxml.Node) []*ooxml.Node {
	if moving == nil || before == nil || moving == before {
		return children
	}
	result := make([]*ooxml.Node, 0, len(children))
	for _, child := range children {
		if child == moving {
			continue
		}
		if child == before {
			result = append(result, moving)
		}
		result = append(result, child)
	}
	if len(result) == len(children)-1 {
		result = append(result, moving)
	}
	return result
}

func smartArtCxnGroupsBySrc(cxnList *ooxml.Node, cxnType string) map[string][]*ooxml.Node {
	result := map[string][]*ooxml.Node{}
	for _, cxn := range cxnList.NamedChildren(ooxml.NSDiagram, "cxn") {
		if cxn.AttrValue("", "type") != cxnType {
			continue
		}
		result[cxn.AttrValue("", "srcId")] = append(result[cxn.AttrValue("", "srcId")], cxn)
	}
	return result
}

func smartArtSortedCxns(cxns []*ooxml.Node) []*ooxml.Node {
	result := append([]*ooxml.Node(nil), cxns...)
	sort.SliceStable(result, func(i, j int) bool {
		return smartArtIntAttr(result[i], "srcOrd") < smartArtIntAttr(result[j], "srcOrd")
	})
	return result
}

func smartArtUniqueIDs(ids []string) []string {
	seen := map[string]bool{}
	var result []string
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		result = append(result, id)
	}
	return result
}

func smartArtIDSet(ids []string) map[string]bool {
	result := map[string]bool{}
	for _, id := range ids {
		if id != "" {
			result[id] = true
		}
	}
	return result
}

func smartArtSortedPointIDs(ptList *ooxml.Node, ids map[string]bool) []string {
	var result []string
	for _, pt := range ptList.NamedChildren(ooxml.NSDiagram, "pt") {
		id := pt.AttrValue("", "modelId")
		if ids[id] {
			result = append(result, id)
		}
	}
	return result
}

func smartArtInsertBeforeFirstPres(ptList *ooxml.Node, nodes ...*ooxml.Node) {
	insertAt := len(ptList.Children)
	for index, child := range ptList.Children {
		if child.Name.Space == ooxml.NSDiagram && child.Name.Local == "pt" && child.AttrValue("", "type") == "pres" {
			insertAt = index
			break
		}
	}
	updated := make([]*ooxml.Node, 0, len(ptList.Children)+len(nodes))
	updated = append(updated, ptList.Children[:insertAt]...)
	updated = append(updated, nodes...)
	updated = append(updated, ptList.Children[insertAt:]...)
	ptList.Children = updated
}

func smartArtKeepChildren(children []*ooxml.Node, removeIDs map[string]bool) []*ooxml.Node {
	kept := children[:0]
	for _, child := range children {
		if removeIDs[child.AttrValue("", "modelId")] {
			continue
		}
		kept = append(kept, child)
	}
	return kept
}

func smartArtRemoveDescendants(root *ooxml.Node, space, local string) {
	kept := root.Children[:0]
	for _, child := range root.Children {
		if child.Name.Space == space && child.Name.Local == local {
			continue
		}
		smartArtRemoveDescendants(child, space, local)
		kept = append(kept, child)
	}
	root.Children = kept
}

func smartArtModelIDs(root *ooxml.Node) map[string]bool {
	result := map[string]bool{}
	for _, node := range root.Descendants(ooxml.NSDiagram, "pt") {
		if id := node.AttrValue("", "modelId"); id != "" {
			result[id] = true
		}
	}
	for _, node := range root.Descendants(ooxml.NSDiagram, "cxn") {
		if id := node.AttrValue("", "modelId"); id != "" {
			result[id] = true
		}
	}
	return result
}

func smartArtNewModelID(used map[string]bool) string {
	for attempt := 0; ; attempt++ {
		id, ok := smartArtRandomModelID()
		if !ok {
			id = fmt.Sprintf("{00000000-0000-4000-8000-%012X}", len(used)+attempt)
		}
		if !used[id] {
			used[id] = true
			return id
		}
	}
}

func smartArtRandomModelID() (string, bool) {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return "", false
	}
	data[6] = (data[6] & 0x0f) | 0x40
	data[8] = (data[8] & 0x3f) | 0x80
	return strings.ToUpper(fmt.Sprintf("{%08x-%04x-%04x-%04x-%012x}",
		data[0:4], data[4:6], data[6:8], data[8:10], data[10:16])), true
}

func smartArtIntAttr(node *ooxml.Node, name string) int {
	value, err := strconv.Atoi(node.AttrValue("", name))
	if err != nil {
		return -1
	}
	return value
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func relatedPartByID(owner string, rels *ooxml.Relationships, id, relType string) (string, error) {
	rel, ok := rels.Find(id)
	if !ok || rel.Type != relType || rel.Mode != ooxml.TargetInternal {
		return "", fmt.Errorf("relationship %s not found", id)
	}
	partName, err := ooxml.ResolveTarget(owner, rel.Target)
	if err != nil {
		return "", err
	}
	return partName, nil
}

func smartArtDrawingRoot(pkg *ooxml.Package, slidePart string, rels *ooxml.Relationships, dataRoot *ooxml.Node) (*ooxml.Node, string, error) {
	ext := dataRoot.FirstDescendant(ooxml.NSDiagram2008, "dataModelExt")
	if ext == nil || ext.AttrValue("", "relId") == "" {
		return nil, "", nil
	}
	partName, err := relatedPartByID(slidePart, rels, ext.AttrValue("", "relId"), ooxml.RelationshipTypeDiagramDrawing)
	if err != nil {
		return nil, "", err
	}
	root, err := pkg.XMLPart(partName)
	if err != nil {
		return nil, "", err
	}
	return root, partName, nil
}

func smartArtNodeIndex(sourceSlide int, shapeID, nodeID string, count int) int {
	prefix := fmt.Sprintf("s%02d_sa%s_n", sourceSlide, shapeID)
	if !strings.HasPrefix(nodeID, prefix) {
		return -1
	}
	value, err := strconv.Atoi(strings.TrimPrefix(nodeID, prefix))
	if err != nil || value < 1 || value > count {
		return -1
	}
	return value - 1
}

func setSmartArtDataText(dataRoot *ooxml.Node, modelID, text string) bool {
	for _, pt := range dataRoot.Descendants(ooxml.NSDiagram, "pt") {
		if pt.AttrValue("", "modelId") != modelID {
			continue
		}
		prSet := pt.Child(ooxml.NSDiagram, "prSet")
		if prSet == nil {
			prSet = ooxml.Element(ooxml.NSDiagram, "prSet")
			pt.Children = append([]*ooxml.Node{prSet}, pt.Children...)
		}
		prSet.RemoveAttr("", "phldr")
		body := pt.Child(ooxml.NSDiagram, "t")
		if body == nil {
			body = ooxml.Element(ooxml.NSDiagram, "t")
			pt.Children = append(pt.Children, body)
		}
		setSmartArtTextBody(body, strings.Split(text, "\n"))
		return true
	}
	return false
}

func setSmartArtDrawingText(drawingRoot *ooxml.Node, presID, text string) bool {
	for _, shape := range drawingRoot.Descendants(ooxml.NSDiagram2008, "sp") {
		if shape.AttrValue("", "modelId") != presID {
			continue
		}
		body := shape.Child(ooxml.NSDiagram2008, "txBody")
		if body == nil {
			return false
		}
		setSmartArtTextBody(body, strings.Split(text, "\n"))
		return true
	}
	return false
}

func setSmartArtTextBody(body *ooxml.Node, lines []string) {
	if len(lines) == 0 {
		lines = []string{""}
	}
	templates := body.NamedChildren(ooxml.NSDrawingML, "p")
	if len(templates) == 0 {
		templates = []*ooxml.Node{ooxml.Element(ooxml.NSDrawingML, "p")}
	}
	for index := range templates {
		templates[index] = templates[index].Clone()
	}
	body.RemoveChildren(ooxml.NSDrawingML, "p")
	for index, line := range lines {
		paragraph := templates[min(index, len(templates)-1)].Clone()
		setParagraphText(paragraph, line)
		body.Children = append(body.Children, paragraph)
	}
}

func smartArtEditText(edit model.SmartArtNodeEdit) string {
	if edit.Paragraphs != nil {
		return strings.Join(edit.Paragraphs, "\n")
	}
	return edit.Text
}

func smartArtStructureOpText(op model.SmartArtStructureOp) string {
	if op.Paragraphs != nil {
		return strings.Join(op.Paragraphs, "\n")
	}
	return op.Text
}

func smartArtSelectors(value model.SmartArtEdit) []string {
	var result []string
	if value.SmartArtID != "" {
		result = append(result, "smartart_id:"+value.SmartArtID)
	}
	if value.ShapeID != "" {
		result = append(result, "shape_id:"+value.ShapeID)
	}
	if value.ShapeName != "" {
		result = append(result, "shape_name:"+value.ShapeName)
	}
	return result
}
