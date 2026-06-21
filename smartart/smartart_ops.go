// Copyright 2026 The OpenAgent Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0

package smartart

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/the-open-agent/office-tool-use/ooxml"
)

func appendSmartArtResizeGroup(dataRoot *ooxml.Node, model *smartArtResizeModel) error {
	switch model.Mode {
	case "top_level_tail":
		return appendSmartArtResizeNode(dataRoot, model)
	case "list_group_tail", "list_single_root_tail":
		return appendSmartArtPromotedTailGroup(dataRoot, model)
	default:
		return appendSmartArtClonedTailGroup(dataRoot, model)
	}
}

func refreshSmartArtResizeModel(dataRoot *ooxml.Node) (*smartArtResizeModel, error) {
	refreshed, reason := smartArtResizeModelFromData(dataRoot)
	if refreshed == nil {
		return nil, fmt.Errorf("%s", reason)
	}
	return refreshed, nil
}

func appendSmartArtClonedTailGroup(dataRoot *ooxml.Node, model *smartArtResizeModel) error {
	if len(model.Groups) < 2 {
		return fmt.Errorf("at least two existing groups are required before append")
	}
	ctx, reason := smartArtResizeContextFromData(dataRoot)
	if reason != "" {
		return fmt.Errorf("%s", reason)
	}
	ids := smartArtModelIDs(dataRoot)
	last := model.Groups[len(model.Groups)-1]
	prev := model.Groups[len(model.Groups)-2]
	if len(prev.TransitionPresIDs) != 0 {
		if err := cloneSmartArtResizeSegment(ctx, ids, prev.TransitionPresIDs, prev.TransitionCxnIDs, map[string]string{
			prev.RootSibTransID: last.RootSibTransID,
		}, nil); err != nil {
			return err
		}
	}
	overrides := map[string]string{}
	for _, id := range append(last.PointIDs, last.PresIDs...) {
		overrides[id] = smartArtNewModelID(ids)
	}
	for _, id := range last.CxnIDs {
		overrides[id] = smartArtNewModelID(ids)
	}
	if err := cloneSmartArtResizeSegment(ctx, ids, append(last.PointIDs, last.PresIDs...), last.CxnIDs, overrides, map[string]string{
		"rootOrd":   strconv.Itoa(len(model.Groups)),
		"rootCxnID": last.RootCxn.AttrValue("", "modelId"),
	}); err != nil {
		return err
	}
	refreshed, err := refreshSmartArtResizeModel(dataRoot)
	if err != nil {
		return err
	}
	smartArtRenumberGenericResizeModel(refreshed)
	if refreshed.Mode == "list_flat_composite_tail" {
		smartArtMoveNewestRootCxnBeforeFirstRootCxn(refreshed)
	}
	return nil
}

func appendSmartArtPromotedTailGroup(dataRoot *ooxml.Node, model *smartArtResizeModel) error {
	if len(model.Groups) == 0 {
		return fmt.Errorf("SmartArt resize groups are missing")
	}
	tail := model.Groups[len(model.Groups)-1]
	if _, err := appendSmartArtChildToParent(dataRoot, model, tail.Root.ContentID); err != nil {
		return err
	}
	refreshed, reason := smartArtResizeModelFromData(dataRoot)
	if refreshed == nil {
		return fmt.Errorf("%s", reason)
	}
	if _, err := appendSmartArtRootOnly(dataRoot, refreshed); err != nil {
		return err
	}
	return nil
}

func appendSmartArtChildToParent(dataRoot *ooxml.Node, model *smartArtResizeModel, parentContentID string) (string, error) {
	if model.Mode != "list_group_tail" && model.Mode != "list_single_root_tail" {
		return "", fmt.Errorf("add_child is supported only for parent/child SmartArt lists")
	}
	targetIndex := -1
	for index, group := range model.Groups {
		if group.Root.ContentID == parentContentID {
			targetIndex = index
			break
		}
	}
	if targetIndex < 0 {
		return "", fmt.Errorf("add_child parent must be a root node")
	}
	ctx, reason := smartArtResizeContextFromData(dataRoot)
	if reason != "" {
		return "", fmt.Errorf("%s", reason)
	}
	ids := smartArtModelIDs(dataRoot)
	target := model.Groups[targetIndex]
	childTemplate, ok := smartArtTailChildTemplate(model.Groups)
	if !ok {
		return "", fmt.Errorf("SmartArt child node template is missing")
	}
	if sharedPresID := smartArtSharedChildTextPresID(ctx, target); sharedPresID != "" {
		newContentID, err := cloneSmartArtResizeDataUnit(ctx, ids, childTemplate, map[string]string{
			"normalSrcID": parentContentID,
			"normalOrd":   strconv.Itoa(len(target.Children)),
		})
		if err != nil {
			return "", err
		}
		if err := appendSmartArtSharedChildPresOf(ctx, ids, target, newContentID, sharedPresID); err != nil {
			return "", err
		}
		refreshed, err := refreshSmartArtResizeModel(dataRoot)
		if err != nil {
			return "", err
		}
		smartArtRenumberGenericResizeModel(refreshed)
		return newContentID, nil
	}
	if previousChild, ok := smartArtLastChild(target); ok {
		transitionTemplate, hasTransition := smartArtVisibleTransitionTemplate(target.Children)
		if !hasTransition {
			transitionTemplate, hasTransition = smartArtVisibleTransitionTemplateForGroups(model.Groups)
		}
		if hasTransition {
			if err := cloneSmartArtResizeSegment(ctx, ids, transitionTemplate.TransitionPresIDs, transitionTemplate.TransitionCxnIDs, map[string]string{
				transitionTemplate.SibTransID: previousChild.SibTransID,
			}, nil); err != nil {
				return "", err
			}
		}
	}
	newContentID, err := cloneSmartArtResizeUnit(ctx, ids, childTemplate, map[string]string{
		"normalSrcID": parentContentID,
		"normalOrd":   strconv.Itoa(len(target.Children)),
	})
	if err != nil {
		return "", err
	}
	refreshed, err := refreshSmartArtResizeModel(dataRoot)
	if err != nil {
		return "", err
	}
	smartArtRenumberGenericResizeModel(refreshed)
	return newContentID, nil
}

func appendSmartArtRootOnly(dataRoot *ooxml.Node, model *smartArtResizeModel) (string, error) {
	if model.Mode != "list_group_tail" && model.Mode != "list_single_root_tail" {
		return "", fmt.Errorf("add_root is supported only for parent/child SmartArt lists")
	}
	if len(model.Groups) == 0 {
		return "", fmt.Errorf("SmartArt resize groups are missing")
	}
	ctx, reason := smartArtResizeContextFromData(dataRoot)
	if reason != "" {
		return "", fmt.Errorf("%s", reason)
	}
	ids := smartArtModelIDs(dataRoot)
	tail := model.Groups[len(model.Groups)-1]
	presIDs := smartArtRootOwnPresIDs(ctx, tail)
	if sharedPresID := smartArtSharedChildTextPresID(ctx, tail); sharedPresID != "" {
		presIDs = smartArtIDsExcept(presIDs, sharedPresID)
	}
	newContentID, err := cloneSmartArtResizeUnitWithPresIDs(ctx, ids, tail.Root, presIDs, map[string]string{
		"normalSrcID": model.DocID,
		"normalOrd":   strconv.Itoa(len(model.Groups)),
	})
	if err != nil {
		return "", err
	}
	refreshed, err := refreshSmartArtResizeModel(dataRoot)
	if err != nil {
		return "", err
	}
	smartArtRenumberGenericResizeModel(refreshed)
	return newContentID, nil
}

func deleteSmartArtResizeTailGroup(dataRoot *ooxml.Node, model *smartArtResizeModel) error {
	switch model.Mode {
	case "top_level_tail":
		return deleteSmartArtResizeTailNode(dataRoot, model)
	case "list_group_tail", "list_single_root_tail":
		return deleteSmartArtPromotedTailGroup(dataRoot, model)
	default:
		return deleteSmartArtClonedTailGroup(dataRoot, model)
	}
}

func deleteSmartArtClonedTailGroup(dataRoot *ooxml.Node, model *smartArtResizeModel) error {
	if len(model.Groups) <= 1 {
		return fmt.Errorf("SmartArt resize cannot delete the last group")
	}
	tail := model.Groups[len(model.Groups)-1]
	prev := model.Groups[len(model.Groups)-2]
	removePointIDs := smartArtIDSet(append(append([]string{}, tail.PointIDs...), tail.PresIDs...))
	for _, id := range prev.TransitionPresIDs {
		removePointIDs[id] = true
	}
	model.PtList.Children = smartArtKeepChildren(model.PtList.Children, removePointIDs)

	removeCxnIDs := smartArtIDSet(tail.CxnIDs)
	for _, id := range prev.TransitionCxnIDs {
		removeCxnIDs[id] = true
	}
	model.CxnList.Children = smartArtKeepChildren(model.CxnList.Children, removeCxnIDs)
	refreshed, err := refreshSmartArtResizeModel(dataRoot)
	if err != nil {
		return err
	}
	smartArtRenumberGenericResizeModel(refreshed)
	return nil
}

func deleteSmartArtPromotedTailGroup(dataRoot *ooxml.Node, model *smartArtResizeModel) error {
	if len(model.Groups) <= 1 {
		return fmt.Errorf("SmartArt resize cannot delete the last group")
	}
	tail := model.Groups[len(model.Groups)-1]
	prev := model.Groups[len(model.Groups)-2]
	if len(tail.Children) != 0 || len(prev.Children) == 0 {
		return fmt.Errorf("SmartArt promoted-tail resize can delete only a trailing empty group")
	}
	removePointIDs := smartArtIDSet(append(append([]string{}, tail.Root.PointIDs...), tail.Root.PresIDs...))
	for _, id := range prev.Children[len(prev.Children)-1].PointIDs {
		removePointIDs[id] = true
	}
	for _, id := range prev.Children[len(prev.Children)-1].PresIDs {
		removePointIDs[id] = true
	}
	if len(prev.Children) > 1 {
		for _, id := range prev.Children[len(prev.Children)-2].TransitionPresIDs {
			removePointIDs[id] = true
		}
	}
	for _, id := range prev.TransitionPresIDs {
		removePointIDs[id] = true
	}
	model.PtList.Children = smartArtKeepChildren(model.PtList.Children, removePointIDs)

	removeCxnIDs := smartArtIDSet(tail.Root.CxnIDs)
	for _, id := range prev.Children[len(prev.Children)-1].CxnIDs {
		removeCxnIDs[id] = true
	}
	if len(prev.Children) > 1 {
		for _, id := range prev.Children[len(prev.Children)-2].TransitionCxnIDs {
			removeCxnIDs[id] = true
		}
	}
	for _, id := range prev.TransitionCxnIDs {
		removeCxnIDs[id] = true
	}
	model.CxnList.Children = smartArtKeepChildren(model.CxnList.Children, removeCxnIDs)
	refreshed, err := refreshSmartArtResizeModel(dataRoot)
	if err != nil {
		return err
	}
	smartArtRenumberGenericResizeModel(refreshed)
	return nil
}

func cloneSmartArtResizeSegment(ctx *smartArtResizeContext, used map[string]bool, pointIDs, cxnIDs []string, overrides, options map[string]string) error {
	pointSet := smartArtIDSet(pointIDs)
	var normalPoints, presPoints []*ooxml.Node
	for _, child := range ctx.PtList.Children {
		id := child.AttrValue("", "modelId")
		if !pointSet[id] {
			continue
		}
		clone := child.Clone()
		smartArtRewriteModelRefs(clone, overrides)
		if clone.AttrValue("", "modelId") == id {
			newID := smartArtNewModelID(used)
			overrides[id] = newID
			clone.SetAttr("", "modelId", newID)
			smartArtRewriteModelRefs(clone, overrides)
		}
		if clone.AttrValue("", "type") == "pres" {
			presPoints = append(presPoints, clone)
		} else {
			normalPoints = append(normalPoints, clone)
		}
	}
	if len(normalPoints) != 0 {
		smartArtInsertBeforeFirstPres(ctx.PtList, normalPoints...)
	}
	ctx.PtList.Children = append(ctx.PtList.Children, presPoints...)

	cxnSet := smartArtIDSet(cxnIDs)
	for _, child := range ctx.CxnList.Children {
		id := child.AttrValue("", "modelId")
		if !cxnSet[id] {
			continue
		}
		clone := child.Clone()
		oldID := id
		smartArtRewriteModelRefs(clone, overrides)
		if clone.AttrValue("", "modelId") == id {
			newID := smartArtNewModelID(used)
			overrides[id] = newID
			clone.SetAttr("", "modelId", newID)
			smartArtRewriteModelRefs(clone, overrides)
		}
		if options != nil && options["rootOrd"] != "" && oldID == options["rootCxnID"] {
			clone.SetAttr("", "srcOrd", options["rootOrd"])
		}
		if options != nil && oldID == options["normalCxnID"] {
			if options["normalSrcID"] != "" {
				clone.SetAttr("", "srcId", options["normalSrcID"])
			}
			if options["normalOrd"] != "" {
				clone.SetAttr("", "srcOrd", options["normalOrd"])
			}
		}
		ctx.CxnList.Children = append(ctx.CxnList.Children, clone)
	}
	return nil
}

func cloneSmartArtResizeUnit(ctx *smartArtResizeContext, used map[string]bool, unit smartArtResizeUnit, options map[string]string) (string, error) {
	return cloneSmartArtResizeUnitWithPresIDs(ctx, used, unit, unit.PresIDs, options)
}

func cloneSmartArtResizeUnitWithPresIDs(ctx *smartArtResizeContext, used map[string]bool, unit smartArtResizeUnit, presIDs []string, options map[string]string) (string, error) {
	if unit.NormalCxn == nil {
		return "", fmt.Errorf("SmartArt resize unit normal connection is missing")
	}
	overrides := map[string]string{}
	presIDs = smartArtUniqueIDs(presIDs)
	pointIDs := append(append([]string{}, unit.PointIDs...), presIDs...)
	for _, id := range pointIDs {
		overrides[id] = smartArtNewModelID(used)
	}
	cxnIDs := []string{unit.NormalCxn.AttrValue("", "modelId")}
	cxnIDs = append(cxnIDs, smartArtCxnIDsForSelectedPres(ctx, presIDs, unit.PointIDs)...)
	cxnIDs = smartArtUniqueIDs(cxnIDs)
	for _, id := range cxnIDs {
		overrides[id] = smartArtNewModelID(used)
	}
	if options == nil {
		options = map[string]string{}
	}
	options["normalCxnID"] = unit.NormalCxn.AttrValue("", "modelId")
	if err := cloneSmartArtResizeSegment(ctx, used, pointIDs, cxnIDs, overrides, options); err != nil {
		return "", err
	}
	return overrides[unit.ContentID], nil
}

func cloneSmartArtResizeDataUnit(ctx *smartArtResizeContext, used map[string]bool, unit smartArtResizeUnit, options map[string]string) (string, error) {
	return cloneSmartArtResizeUnitWithPresIDs(ctx, used, unit, nil, options)
}

func smartArtCxnIDsForSelectedPres(ctx *smartArtResizeContext, presIDs, pointIDs []string) []string {
	presSet := smartArtIDSet(presIDs)
	pointSet := smartArtIDSet(pointIDs)
	var result []string
	for _, cxn := range ctx.CxnList.NamedChildren(ooxml.NSDiagram, "cxn") {
		id := cxn.AttrValue("", "modelId")
		if id == "" {
			continue
		}
		switch cxn.AttrValue("", "type") {
		case "presOf":
			if pointSet[cxn.AttrValue("", "srcId")] && presSet[cxn.AttrValue("", "destId")] {
				result = append(result, id)
			}
		case "presParOf":
			if presSet[cxn.AttrValue("", "srcId")] && presSet[cxn.AttrValue("", "destId")] {
				result = append(result, id)
			}
		}
	}
	return result
}

func appendSmartArtSharedChildPresOf(ctx *smartArtResizeContext, used map[string]bool, group smartArtResizeGroup, newContentID, presID string) error {
	if len(group.Children) == 0 {
		return fmt.Errorf("SmartArt shared child presentation template is missing")
	}
	var template *ooxml.Node
	for _, cxn := range ctx.PresOfBySrc[group.Children[0].ContentID] {
		if cxn.AttrValue("", "destId") == presID {
			template = cxn
			break
		}
	}
	if template == nil {
		for _, cxn := range ctx.PresOfBySrc[group.Root.ContentID] {
			if cxn.AttrValue("", "destId") == presID {
				template = cxn
				break
			}
		}
	}
	if template == nil {
		return fmt.Errorf("SmartArt shared child presentation connection is missing")
	}
	clone := template.Clone()
	clone.SetAttr("", "modelId", smartArtNewModelID(used))
	clone.SetAttr("", "srcId", newContentID)
	clone.SetAttr("", "destId", presID)
	ctx.CxnList.Children = append(ctx.CxnList.Children, clone)
	return nil
}

func smartArtSharedChildTextPresID(ctx *smartArtResizeContext, group smartArtResizeGroup) string {
	for _, presID := range group.Root.PresIDs {
		pt := ctx.Points[presID]
		if pt == nil {
			continue
		}
		prSet := pt.Child(ooxml.NSDiagram, "prSet")
		if prSet == nil || prSet.AttrValue("", "presAssocID") != group.Root.ContentID {
			continue
		}
		if strings.EqualFold(prSet.AttrValue("", "presName"), "childText") {
			return presID
		}
	}
	return ""
}

func smartArtRootOwnPresIDs(ctx *smartArtResizeContext, group smartArtResizeGroup) []string {
	result := make([]string, 0, len(group.Root.PresIDs))
	for _, presID := range group.Root.PresIDs {
		pt := ctx.Points[presID]
		if pt == nil {
			continue
		}
		prSet := pt.Child(ooxml.NSDiagram, "prSet")
		if prSet == nil {
			result = append(result, presID)
			continue
		}
		if prSet.AttrValue("", "presAssocID") == group.Root.ContentID && !smartArtPresNameLooksLikeChild(prSet.AttrValue("", "presName")) {
			result = append(result, presID)
		}
	}
	return result
}

func smartArtPresNameLooksLikeChild(name string) bool {
	normalized := strings.ToLower(name)
	return strings.Contains(normalized, "child") ||
		strings.Contains(normalized, "descendant")
}

func smartArtIDsExcept(ids []string, exclude string) []string {
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != exclude {
			result = append(result, id)
		}
	}
	return result
}

func smartArtTailChildTemplate(groups []smartArtResizeGroup) (smartArtResizeUnit, bool) {
	for index := len(groups) - 1; index >= 0; index-- {
		if child, ok := smartArtLastChild(groups[index]); ok {
			return child, true
		}
	}
	return smartArtResizeUnit{}, false
}

func smartArtLastChild(group smartArtResizeGroup) (smartArtResizeUnit, bool) {
	if len(group.Children) == 0 {
		return smartArtResizeUnit{}, false
	}
	return group.Children[len(group.Children)-1], true
}

func smartArtVisibleTransitionTemplate(units []smartArtResizeUnit) (smartArtResizeUnit, bool) {
	for index := len(units) - 1; index >= 0; index-- {
		if len(units[index].TransitionPresIDs) != 0 {
			return units[index], true
		}
	}
	return smartArtResizeUnit{}, false
}

func smartArtVisibleTransitionTemplateForGroups(groups []smartArtResizeGroup) (smartArtResizeUnit, bool) {
	for groupIndex := len(groups) - 1; groupIndex >= 0; groupIndex-- {
		if unit, ok := smartArtVisibleTransitionTemplate(groups[groupIndex].Children); ok {
			return unit, true
		}
	}
	return smartArtResizeUnit{}, false
}

func smartArtVisibleRootTransitionTemplate(groups []smartArtResizeGroup) (smartArtResizeUnit, bool) {
	for index := len(groups) - 1; index >= 0; index-- {
		if len(groups[index].Root.TransitionPresIDs) != 0 {
			return groups[index].Root, true
		}
	}
	return smartArtResizeUnit{}, false
}

func smartArtRewriteModelRefs(node *ooxml.Node, ids map[string]string) {
	for index := range node.Attr {
		if replacement := ids[node.Attr[index].Value]; replacement != "" {
			node.Attr[index].Value = replacement
		}
	}
	for _, child := range node.Children {
		smartArtRewriteModelRefs(child, ids)
	}
}

func appendSmartArtResizeNode(dataRoot *ooxml.Node, model *smartArtResizeModel) error {
	if len(model.Nodes) < 2 {
		return fmt.Errorf("at least two existing nodes are required before append")
	}
	last := model.Nodes[len(model.Nodes)-1]
	prevVisible := model.Nodes[len(model.Nodes)-2]
	if prevVisible.PresSibTransPt == nil {
		return fmt.Errorf("visible sibling transition template is missing")
	}
	ids := smartArtModelIDs(dataRoot)
	newContentID := smartArtNewModelID(ids)
	newParID := smartArtNewModelID(ids)
	newSibID := smartArtNewModelID(ids)
	newCxnID := smartArtNewModelID(ids)
	newPresNodeID := smartArtNewModelID(ids)
	newPresOfID := smartArtNewModelID(ids)
	newPresParNodeID := smartArtNewModelID(ids)
	newPresSibID := smartArtNewModelID(ids)
	newPresParSibID := smartArtNewModelID(ids)

	newContent := last.ContentPt.Clone()
	newContent.SetAttr("", "modelId", newContentID)
	newPar := last.ParTransPt.Clone()
	newPar.SetAttr("", "modelId", newParID)
	newPar.SetAttr("", "cxnId", newCxnID)
	newSib := last.SibTransPt.Clone()
	newSib.SetAttr("", "modelId", newSibID)
	newSib.SetAttr("", "cxnId", newCxnID)
	smartArtInsertBeforeFirstPres(model.PtList, newContent, newPar, newSib)

	newPresSib := prevVisible.PresSibTransPt.Clone()
	newPresSib.SetAttr("", "modelId", newPresSibID)
	if prSet := newPresSib.Child(ooxml.NSDiagram, "prSet"); prSet != nil {
		prSet.SetAttr("", "presAssocID", last.SibTransID)
		prSet.SetAttr("", "presName", "sibTrans")
		prSet.SetAttr("", "presStyleCnt", "0")
		prSet.RemoveAttr("", "presStyleLbl")
		prSet.RemoveAttr("", "presStyleIdx")
	}
	newPresNode := last.PresNodePt.Clone()
	newPresNode.SetAttr("", "modelId", newPresNodeID)
	if prSet := newPresNode.Child(ooxml.NSDiagram, "prSet"); prSet != nil {
		prSet.SetAttr("", "presAssocID", newContentID)
	}
	model.PtList.Children = append(model.PtList.Children, newPresSib, newPresNode)

	newNormal := last.NormalCxn.Clone()
	newNormal.SetAttr("", "modelId", newCxnID)
	newNormal.SetAttr("", "destId", newContentID)
	newNormal.SetAttr("", "srcOrd", strconv.Itoa(len(model.Nodes)))
	newNormal.SetAttr("", "parTransId", newParID)
	newNormal.SetAttr("", "sibTransId", newSibID)
	newPresOf := last.PresOfCxn.Clone()
	newPresOf.SetAttr("", "modelId", newPresOfID)
	newPresOf.SetAttr("", "srcId", newContentID)
	newPresOf.SetAttr("", "destId", newPresNodeID)
	newSibPresParOf := last.NodePresParOf.Clone()
	newSibPresParOf.SetAttr("", "modelId", newPresParSibID)
	newSibPresParOf.SetAttr("", "destId", newPresSibID)
	newNodePresParOf := last.NodePresParOf.Clone()
	newNodePresParOf.SetAttr("", "modelId", newPresParNodeID)
	newNodePresParOf.SetAttr("", "destId", newPresNodeID)
	model.CxnList.Children = append(model.CxnList.Children, newNormal, newPresOf, newSibPresParOf, newNodePresParOf)
	refreshed, err := refreshSmartArtResizeModel(dataRoot)
	if err != nil {
		return err
	}
	smartArtRenumberResizeModel(refreshed)
	return nil
}

func deleteSmartArtResizeTailNode(dataRoot *ooxml.Node, model *smartArtResizeModel) error {
	if len(model.Nodes) <= 1 {
		return fmt.Errorf("SmartArt resize cannot delete the last node")
	}
	tail := model.Nodes[len(model.Nodes)-1]
	prev := model.Nodes[len(model.Nodes)-2]
	removeIDs := map[string]bool{
		tail.ContentID: true, tail.ParTransID: true, tail.SibTransID: true, tail.PresNodeID: true,
	}
	if prev.PresSibTransID != "" {
		removeIDs[prev.PresSibTransID] = true
	}
	model.PtList.Children = smartArtKeepChildren(model.PtList.Children, removeIDs)

	removeCxnIDs := map[string]bool{
		tail.CxnID:                                  true,
		tail.PresOfCxn.AttrValue("", "modelId"):     true,
		tail.NodePresParOf.AttrValue("", "modelId"): true,
	}
	if prev.SibTransPresParOf != nil {
		removeCxnIDs[prev.SibTransPresParOf.AttrValue("", "modelId")] = true
	}
	model.CxnList.Children = smartArtKeepChildren(model.CxnList.Children, removeCxnIDs)
	refreshed, err := refreshSmartArtResizeModel(dataRoot)
	if err != nil {
		return err
	}
	smartArtRenumberResizeModel(refreshed)
	return nil
}
