// Copyright 2026 The OpenAgent Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0

package smartart

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/the-open-agent/office-tool-use/model"
	"github.com/the-open-agent/office-tool-use/ooxml"
)

type smartArtNodeRef struct {
	ModelID string
	PresIDs []string
	Index   int
	Text    string
}

type smartArtPresCandidate struct {
	ID    string
	Index int
	Score int
}

type smartArtResizeNode struct {
	ContentID         string
	ParTransID        string
	SibTransID        string
	CxnID             string
	PresNodeID        string
	PresSibTransID    string
	ContentPt         *ooxml.Node
	ParTransPt        *ooxml.Node
	SibTransPt        *ooxml.Node
	PresNodePt        *ooxml.Node
	PresSibTransPt    *ooxml.Node
	NormalCxn         *ooxml.Node
	PresOfCxn         *ooxml.Node
	NodePresParOf     *ooxml.Node
	SibTransPresParOf *ooxml.Node
}

type smartArtResizeGroup struct {
	ContentIDs        []string
	PointIDs          []string
	CxnIDs            []string
	PresIDs           []string
	TransitionPresIDs []string
	TransitionCxnIDs  []string
	RootCxn           *ooxml.Node
	RootSibTransID    string
	Root              smartArtResizeUnit
	Children          []smartArtResizeUnit
}

type smartArtResizeUnit struct {
	ContentID         string
	PointIDs          []string
	CxnIDs            []string
	PresIDs           []string
	TransitionPresIDs []string
	TransitionCxnIDs  []string
	NormalCxn         *ooxml.Node
	SibTransID        string
}

type smartArtResizeModel struct {
	PtList        *ooxml.Node
	CxnList       *ooxml.Node
	DocID         string
	DiagramPresID string
	Mode          string
	Nodes         []smartArtResizeNode
	Groups        []smartArtResizeGroup
	FixedNodes    int
	GroupNodes    int
	ResizeStep    int
}

func AnalyzeSmartArts(pkg *ooxml.Package, slide *ooxml.Node, ref ooxml.SlideRef, objectByID map[string]*model.SlideObject) ([]model.SmartArtInfo, error) {
	rels, err := pkg.Relationships(ref.PartName)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]ooxml.Relationship, len(rels.Items))
	for _, rel := range rels.Items {
		byID[rel.ID] = rel
	}

	frames := smartArtFrames(slide)
	result := make([]model.SmartArtInfo, 0, len(frames))
	for order, frame := range frames {
		shapeID, shapeName := ooxml.ShapeIdentity(frame, order+1)
		info := model.SmartArtInfo{
			SmartArtID: fmt.Sprintf("s%02d_sa%s", ref.Index, shapeID),
			ShapeID:    shapeID,
			ShapeName:  shapeName,
			Editable:   true,
		}
		if object := objectByID[shapeID]; object != nil {
			info.Geometry = object.Geometry
		} else {
			info.Geometry = ooxml.ContainerGeometry(frame)
		}

		dataRelID := smartArtRelIDs(frame).AttrValue(ooxml.NSOfficeRels, "dm")
		rel, ok := byID[dataRelID]
		if !ok || rel.Type != ooxml.RelationshipTypeDiagramData || rel.Mode != ooxml.TargetInternal {
			info.Editable = false
			info.Reason = "diagram data relationship not found"
			result = append(result, info)
			continue
		}
		dataPart, err := ooxml.ResolveTarget(ref.PartName, rel.Target)
		if err != nil || !pkg.HasPart(dataPart) {
			info.Editable = false
			info.Reason = "diagram data part not found"
			result = append(result, info)
			continue
		}
		dataRoot, err := pkg.XMLPart(dataPart)
		if err != nil {
			info.Editable = false
			info.Reason = "diagram data part cannot be parsed"
			result = append(result, info)
			continue
		}
		drawingRoot, _, err := smartArtDrawingRoot(pkg, ref.PartName, rels, dataRoot)
		if err != nil {
			info.Editable = false
			info.Reason = "diagram drawing part cannot be resolved: " + err.Error()
			result = append(result, info)
			continue
		}
		nodes := smartArtNodeRefs(dataRoot, drawingRoot)
		if len(nodes) == 0 {
			info.Editable = false
			info.Reason = "editable SmartArt nodes not found"
		}
		for index, node := range nodes {
			info.Nodes = append(info.Nodes, model.SmartArtNodeInfo{
				NodeID:         fmt.Sprintf("s%02d_sa%s_n%02d", ref.Index, shapeID, index+1),
				Text:           node.Text,
				ParagraphCount: max(len(strings.Split(node.Text, "\n")), 1),
				Editable:       true,
				ModelID:        node.ModelID,
				PresIDs:        node.PresIDs,
			})
		}
		if model, reason := smartArtResizeModelFromData(dataRoot); model != nil {
			info.Resizable = true
			info.ResizeMode = model.Mode
			info.Structure = smartArtStructureInfo(model, info.Nodes)
		} else {
			info.ResizeReason = reason
		}
		result = append(result, info)
	}
	return result, nil
}

func smartArtFrames(root *ooxml.Node) []*ooxml.Node {
	var result []*ooxml.Node
	for _, frame := range root.Descendants(ooxml.NSPresentation, "graphicFrame") {
		data := frame.FirstDescendant(ooxml.NSDrawingML, "graphicData")
		if data != nil && data.AttrValue("", "uri") == "http://schemas.openxmlformats.org/drawingml/2006/diagram" && smartArtRelIDs(frame) != nil {
			result = append(result, frame)
		}
	}
	return result
}

func smartArtRelIDs(frame *ooxml.Node) *ooxml.Node {
	return frame.FirstDescendant(ooxml.NSDiagram, "relIds")
}

func smartArtNodeRefs(dataRoot, drawingRoot *ooxml.Node) []smartArtNodeRef {
	textShapeIDs := smartArtDrawingTextShapeIDs(drawingRoot)
	hasDrawingCache := drawingRoot != nil
	candidatesByContent := map[string][]smartArtPresCandidate{}
	presAttrs := map[string]*ooxml.Node{}
	for _, pt := range dataRoot.Descendants(ooxml.NSDiagram, "pt") {
		if pt.AttrValue("", "type") != "pres" {
			continue
		}
		prSet := pt.Child(ooxml.NSDiagram, "prSet")
		if prSet == nil {
			continue
		}
		contentID := prSet.AttrValue("", "presAssocID")
		if contentID == "" {
			continue
		}
		presID := pt.AttrValue("", "modelId")
		if presID == "" {
			continue
		}
		presAttrs[presID] = prSet
		candidatesByContent[contentID] = append(candidatesByContent[contentID], smartArtPresCandidate{
			ID:    presID,
			Index: smartArtPresStyleIndex(prSet),
			Score: smartArtPresScore(prSet, textShapeIDs[presID]),
		})
	}
	for _, cxn := range dataRoot.Descendants(ooxml.NSDiagram, "cxn") {
		if cxn.AttrValue("", "type") != "presOf" {
			continue
		}
		contentID, presID := cxn.AttrValue("", "srcId"), cxn.AttrValue("", "destId")
		if contentID == "" || presID == "" || presAttrs[presID] == nil {
			continue
		}
		prSet := presAttrs[presID]
		candidatesByContent[contentID] = append(candidatesByContent[contentID], smartArtPresCandidate{
			ID:    presID,
			Index: smartArtPresStyleIndex(prSet),
			Score: smartArtPresScore(prSet, textShapeIDs[presID]),
		})
	}

	var nodes []smartArtNodeRef
	contentOrder := 0
	for _, pt := range dataRoot.Descendants(ooxml.NSDiagram, "pt") {
		modelID := pt.AttrValue("", "modelId")
		if modelID == "" || pt.AttrValue("", "type") != "" {
			continue
		}
		candidates := smartArtBestPresCandidates(candidatesByContent[modelID])
		if len(candidates) == 0 && !hasDrawingCache {
			candidates = smartArtFallbackPresCandidates(candidatesByContent[modelID])
		}
		if len(candidates) == 0 {
			if !hasDrawingCache && pt.Child(ooxml.NSDiagram, "t") != nil {
				nodes = append(nodes, smartArtNodeRef{
					ModelID: modelID,
					Index:   contentOrder,
					Text:    strings.Join(ooxml.ParagraphTexts(pt.Child(ooxml.NSDiagram, "t")), "\n"),
				})
			}
			contentOrder++
			continue
		}
		index := contentOrder
		if candidates[0].Index >= 0 {
			index = candidates[0].Index
		}
		nodes = append(nodes, smartArtNodeRef{
			ModelID: modelID,
			PresIDs: smartArtCandidateIDs(candidates),
			Index:   index,
			Text:    strings.Join(ooxml.ParagraphTexts(pt.Child(ooxml.NSDiagram, "t")), "\n"),
		})
		contentOrder++
	}
	sort.SliceStable(nodes, func(i, j int) bool {
		return nodes[i].Index < nodes[j].Index
	})
	return nodes
}

func smartArtFallbackPresCandidates(candidates []smartArtPresCandidate) []smartArtPresCandidate {
	unique := make([]smartArtPresCandidate, 0, len(candidates))
	seen := map[string]bool{}
	for _, candidate := range candidates {
		if candidate.ID == "" || seen[candidate.ID] {
			continue
		}
		seen[candidate.ID] = true
		unique = append(unique, candidate)
	}
	sort.SliceStable(unique, func(i, j int) bool {
		if unique[i].Index != unique[j].Index {
			return unique[i].Index < unique[j].Index
		}
		return unique[i].ID < unique[j].ID
	})
	return unique
}

func smartArtDrawingTextShapeIDs(root *ooxml.Node) map[string]bool {
	result := map[string]bool{}
	if root == nil {
		return result
	}
	for _, shape := range root.Descendants(ooxml.NSDiagram2008, "sp") {
		modelID := shape.AttrValue("", "modelId")
		if modelID != "" && shape.Child(ooxml.NSDiagram2008, "txBody") != nil {
			result[modelID] = true
		}
	}
	return result
}

func smartArtPresStyleIndex(prSet *ooxml.Node) int {
	raw := prSet.AttrValue("", "presStyleIdx")
	value, err := strconv.Atoi(raw)
	if err != nil {
		return -1
	}
	return value
}

func smartArtPresScore(prSet *ooxml.Node, hasDrawingText bool) int {
	score := 0
	if hasDrawingText {
		score += 100
	}
	presName := prSet.AttrValue("", "presName")
	styleLabel := prSet.AttrValue("", "presStyleLbl")
	if styleLabel == "node1" {
		score += 60
	}
	if presName == "node" {
		score += 50
	}
	if strings.HasSuffix(presName, "Tx") || strings.Contains(strings.ToLower(presName), "text") {
		score += 25
	}
	lowerName := strings.ToLower(presName)
	lowerLabel := strings.ToLower(styleLabel)
	if strings.Contains(lowerName, "dummy") || strings.Contains(lowerName, "space") ||
		strings.Contains(lowerName, "arrow") || strings.Contains(lowerName, "trans") {
		score -= 80
	}
	if strings.Contains(lowerLabel, "revtx") || strings.Contains(lowerLabel, "trans") {
		score -= 40
	}
	return score
}

func smartArtBestPresCandidates(candidates []smartArtPresCandidate) []smartArtPresCandidate {
	if len(candidates) == 0 {
		return nil
	}
	unique := make([]smartArtPresCandidate, 0, len(candidates))
	seen := map[string]bool{}
	for _, candidate := range candidates {
		if candidate.ID == "" || seen[candidate.ID] {
			continue
		}
		seen[candidate.ID] = true
		unique = append(unique, candidate)
	}
	sort.SliceStable(unique, func(i, j int) bool {
		if unique[i].Score != unique[j].Score {
			return unique[i].Score > unique[j].Score
		}
		return unique[i].Index < unique[j].Index
	})
	if len(unique) == 0 || unique[0].Score <= 0 {
		return nil
	}
	bestScore := unique[0].Score
	var result []smartArtPresCandidate
	for _, candidate := range unique {
		if candidate.Score != bestScore {
			break
		}
		result = append(result, candidate)
	}
	return result
}

func smartArtCandidateIDs(candidates []smartArtPresCandidate) []string {
	result := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		result = append(result, candidate.ID)
	}
	return result
}
