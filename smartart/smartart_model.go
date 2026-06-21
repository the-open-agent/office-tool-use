// Copyright 2026 The OpenAgent Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0

package smartart

import (
	"sort"

	"github.com/the-open-agent/office-tool-use/ooxml"
)

func smartArtResizeModelFromData(dataRoot *ooxml.Node) (*smartArtResizeModel, string) {
	if model, reason := topLevelTailSmartArtResizeModelFromData(dataRoot); model != nil {
		return model, ""
	} else if reason == "diagram point list or connection list is missing" {
		return nil, reason
	}
	if model, reason := genericListSmartArtResizeModelFromData(dataRoot, "list_flat_composite_tail"); model != nil {
		return model, ""
	} else if reason == "diagram point list or connection list is missing" || reason == "diagram root nodes are missing" {
		return nil, reason
	}
	if model, reason := genericListSmartArtResizeModelFromData(dataRoot, "list_group_tail"); model != nil {
		return model, ""
	} else if reason == "diagram point list or connection list is missing" || reason == "diagram root nodes are missing" {
		return nil, reason
	}
	if model, reason := genericListSmartArtResizeModelFromData(dataRoot, "list_single_root_tail"); model != nil {
		return model, ""
	} else {
		return nil, reason
	}
}

func topLevelTailSmartArtResizeModelFromData(dataRoot *ooxml.Node) (*smartArtResizeModel, string) {
	ptList := dataRoot.Child(ooxml.NSDiagram, "ptLst")
	cxnList := dataRoot.Child(ooxml.NSDiagram, "cxnLst")
	if ptList == nil || cxnList == nil {
		return nil, "diagram point list or connection list is missing"
	}

	points := map[string]*ooxml.Node{}
	var docID, diagramPresID string
	presNodes := map[string]*ooxml.Node{}
	presSibTrans := map[string]*ooxml.Node{}
	for _, pt := range ptList.NamedChildren(ooxml.NSDiagram, "pt") {
		id := pt.AttrValue("", "modelId")
		if id == "" {
			continue
		}
		points[id] = pt
		if pt.AttrValue("", "type") == "doc" {
			docID = id
			continue
		}
		if pt.AttrValue("", "type") != "pres" {
			continue
		}
		prSet := pt.Child(ooxml.NSDiagram, "prSet")
		if prSet == nil {
			continue
		}
		switch prSet.AttrValue("", "presName") {
		case "diagram":
			if prSet.AttrValue("", "presAssocID") == docID || docID == "" {
				diagramPresID = id
			}
		case "node":
			presNodes[prSet.AttrValue("", "presAssocID")] = pt
		case "sibTrans":
			presSibTrans[prSet.AttrValue("", "presAssocID")] = pt
		}
	}
	if docID == "" || diagramPresID == "" {
		return nil, "diagram root nodes are missing"
	}

	var normalCxns []*ooxml.Node
	presOf := map[string]*ooxml.Node{}
	nodePresParOf := map[string]*ooxml.Node{}
	sibTransPresParOf := map[string]*ooxml.Node{}
	for _, cxn := range cxnList.NamedChildren(ooxml.NSDiagram, "cxn") {
		switch cxn.AttrValue("", "type") {
		case "":
			if cxn.AttrValue("", "srcId") == docID {
				normalCxns = append(normalCxns, cxn)
			}
		case "presOf":
			presOf[cxn.AttrValue("", "srcId")] = cxn
		case "presParOf":
			if cxn.AttrValue("", "srcId") != diagramPresID {
				continue
			}
			destID := cxn.AttrValue("", "destId")
			for assocID, presPt := range presNodes {
				if presPt.AttrValue("", "modelId") == destID {
					nodePresParOf[assocID] = cxn
					break
				}
			}
			for assocID, presPt := range presSibTrans {
				if presPt.AttrValue("", "modelId") == destID {
					sibTransPresParOf[assocID] = cxn
					break
				}
			}
		}
	}
	sort.SliceStable(normalCxns, func(i, j int) bool {
		return smartArtIntAttr(normalCxns[i], "srcOrd") < smartArtIntAttr(normalCxns[j], "srcOrd")
	})
	if len(normalCxns) < 2 {
		return nil, "at least two top-level SmartArt nodes are required for tail resize"
	}

	model := &smartArtResizeModel{PtList: ptList, CxnList: cxnList, DocID: docID, DiagramPresID: diagramPresID, Mode: "top_level_tail", GroupNodes: 1, ResizeStep: 1}
	seenContent := map[string]bool{}
	for index, cxn := range normalCxns {
		if smartArtIntAttr(cxn, "srcOrd") != index {
			return nil, "top-level node order is not contiguous"
		}
		contentID := cxn.AttrValue("", "destId")
		parTransID := cxn.AttrValue("", "parTransId")
		sibTransID := cxn.AttrValue("", "sibTransId")
		presNodePt := presNodes[contentID]
		presOfCxn := presOf[contentID]
		if contentID == "" || parTransID == "" || sibTransID == "" || seenContent[contentID] ||
			points[contentID] == nil || points[parTransID] == nil || points[sibTransID] == nil ||
			presNodePt == nil || presOfCxn == nil || nodePresParOf[contentID] == nil {
			return nil, "top-level node mapping is incomplete"
		}
		if points[parTransID].AttrValue("", "type") != "parTrans" || points[sibTransID].AttrValue("", "type") != "sibTrans" {
			return nil, "transition node mapping is incomplete"
		}
		if index < len(normalCxns)-1 && (presSibTrans[sibTransID] == nil || sibTransPresParOf[sibTransID] == nil) {
			return nil, "visible sibling transition mapping is incomplete"
		}
		if index == len(normalCxns)-1 && presSibTrans[sibTransID] != nil {
			return nil, "last sibling transition is already visible"
		}
		seenContent[contentID] = true
		model.Nodes = append(model.Nodes, smartArtResizeNode{
			ContentID: contentID, ParTransID: parTransID, SibTransID: sibTransID, CxnID: cxn.AttrValue("", "modelId"),
			PresNodeID: presNodePt.AttrValue("", "modelId"), ContentPt: points[contentID], ParTransPt: points[parTransID],
			SibTransPt: points[sibTransID], PresNodePt: presNodePt, NormalCxn: cxn, PresOfCxn: presOfCxn,
			NodePresParOf: nodePresParOf[contentID], PresSibTransID: "",
			SibTransPresParOf: sibTransPresParOf[sibTransID],
		})
		if presPt := presSibTrans[sibTransID]; presPt != nil {
			model.Nodes[len(model.Nodes)-1].PresSibTransID = presPt.AttrValue("", "modelId")
			model.Nodes[len(model.Nodes)-1].PresSibTransPt = presPt
			model.Nodes[len(model.Nodes)-1].SibTransPresParOf = sibTransPresParOf[sibTransID]
		}
	}
	return model, ""
}

type smartArtResizeContext struct {
	PtList        *ooxml.Node
	CxnList       *ooxml.Node
	Points        map[string]*ooxml.Node
	DocID         string
	DiagramPresID string
	NormalBySrc   map[string][]*ooxml.Node
	PresOfBySrc   map[string][]*ooxml.Node
	PresParBySrc  map[string][]*ooxml.Node
	PresByAssoc   map[string][]*ooxml.Node
}

func genericListSmartArtResizeModelFromData(dataRoot *ooxml.Node, mode string) (*smartArtResizeModel, string) {
	ctx, reason := smartArtResizeContextFromData(dataRoot)
	if reason != "" {
		return nil, reason
	}
	rootCxns := smartArtSortedCxns(ctx.NormalBySrc[ctx.DocID])
	if len(rootCxns) == 0 {
		return nil, "top-level SmartArt nodes are missing"
	}
	model := &smartArtResizeModel{
		PtList: ctx.PtList, CxnList: ctx.CxnList, DocID: ctx.DocID, DiagramPresID: ctx.DiagramPresID,
		Mode: mode,
	}

	switch mode {
	case "list_flat_composite_tail":
		if len(rootCxns) < 2 {
			return nil, "at least two flat SmartArt nodes are required for tail resize"
		}
		for _, cxn := range rootCxns {
			if len(ctx.NormalBySrc[cxn.AttrValue("", "destId")]) != 0 {
				return nil, "SmartArt is not a flat list"
			}
			group := smartArtResizeGroupFromRootCxn(ctx, cxn, nil)
			if len(group.ContentIDs) != 1 {
				return nil, "flat SmartArt group mapping is incomplete"
			}
			model.Groups = append(model.Groups, group)
		}
		model.GroupNodes = 1
		model.ResizeStep = 1
	case "list_group_tail":
		if len(rootCxns) < 2 {
			return nil, "at least two SmartArt groups are required for tail resize"
		}
		hasChildTemplate := false
		for _, cxn := range rootCxns {
			children := smartArtSortedCxns(ctx.NormalBySrc[cxn.AttrValue("", "destId")])
			if len(children) != 0 {
				hasChildTemplate = true
			}
			for _, child := range children {
				if len(ctx.NormalBySrc[child.AttrValue("", "destId")]) != 0 {
					return nil, "nested SmartArt groups are not supported"
				}
			}
			model.Groups = append(model.Groups, smartArtResizeGroupFromRootCxn(ctx, cxn, children))
		}
		if !hasChildTemplate {
			return nil, "SmartArt child node template is missing"
		}
		model.GroupNodes = 2
		model.ResizeStep = 2
	case "list_single_root_tail":
		if len(rootCxns) != 1 {
			return nil, "SmartArt is not a single-root list"
		}
		containerID := rootCxns[0].AttrValue("", "destId")
		children := smartArtSortedCxns(ctx.NormalBySrc[containerID])
		if len(children) < 1 {
			return nil, "single-root SmartArt list items are missing"
		}
		model.FixedNodes = 1
		model.GroupNodes = 1
		model.ResizeStep = 2
		for _, child := range children {
			if len(ctx.NormalBySrc[child.AttrValue("", "destId")]) != 0 {
				return nil, "nested SmartArt list items are not supported"
			}
		}
		model.Groups = append(model.Groups, smartArtResizeGroupFromRootCxn(ctx, rootCxns[0], children))
	default:
		return nil, "unsupported SmartArt resize model"
	}

	if len(model.Groups) == 0 {
		return nil, "SmartArt resize groups are missing"
	}
	if model.GroupNodes == 0 {
		model.GroupNodes = len(model.Groups[0].ContentIDs)
	}
	for _, group := range model.Groups {
		if model.Mode == "list_flat_composite_tail" && len(group.ContentIDs) != model.GroupNodes {
			return nil, "SmartArt group content count is not consistent"
		}
		for _, contentID := range group.ContentIDs {
			model.Nodes = append(model.Nodes, smartArtResizeNode{ContentID: contentID, ContentPt: ctx.Points[contentID]})
		}
	}
	if len(model.Nodes) < 2 {
		return nil, "at least two editable SmartArt nodes are required for tail resize"
	}
	if model.ResizeStep == 0 {
		model.ResizeStep = model.GroupNodes
	}
	return model, ""
}

func smartArtResizeContextFromData(dataRoot *ooxml.Node) (*smartArtResizeContext, string) {
	ptList := dataRoot.Child(ooxml.NSDiagram, "ptLst")
	cxnList := dataRoot.Child(ooxml.NSDiagram, "cxnLst")
	if ptList == nil || cxnList == nil {
		return nil, "diagram point list or connection list is missing"
	}
	ctx := &smartArtResizeContext{
		PtList: ptList, CxnList: cxnList, Points: map[string]*ooxml.Node{}, NormalBySrc: map[string][]*ooxml.Node{},
		PresOfBySrc: map[string][]*ooxml.Node{}, PresParBySrc: map[string][]*ooxml.Node{}, PresByAssoc: map[string][]*ooxml.Node{},
	}
	for _, pt := range ptList.NamedChildren(ooxml.NSDiagram, "pt") {
		id := pt.AttrValue("", "modelId")
		if id == "" {
			continue
		}
		ctx.Points[id] = pt
		if pt.AttrValue("", "type") == "doc" {
			ctx.DocID = id
		}
		if pt.AttrValue("", "type") != "pres" {
			continue
		}
		prSet := pt.Child(ooxml.NSDiagram, "prSet")
		if prSet == nil {
			continue
		}
		assocID := prSet.AttrValue("", "presAssocID")
		if assocID != "" {
			ctx.PresByAssoc[assocID] = append(ctx.PresByAssoc[assocID], pt)
		}
		if prSet.AttrValue("", "presName") == "diagram" {
			ctx.DiagramPresID = id
		}
	}
	for _, cxn := range cxnList.NamedChildren(ooxml.NSDiagram, "cxn") {
		switch cxn.AttrValue("", "type") {
		case "":
			ctx.NormalBySrc[cxn.AttrValue("", "srcId")] = append(ctx.NormalBySrc[cxn.AttrValue("", "srcId")], cxn)
		case "presOf":
			ctx.PresOfBySrc[cxn.AttrValue("", "srcId")] = append(ctx.PresOfBySrc[cxn.AttrValue("", "srcId")], cxn)
		case "presParOf":
			ctx.PresParBySrc[cxn.AttrValue("", "srcId")] = append(ctx.PresParBySrc[cxn.AttrValue("", "srcId")], cxn)
		}
	}
	if ctx.DiagramPresID == "" {
		for _, cxn := range ctx.PresOfBySrc[ctx.DocID] {
			if destID := cxn.AttrValue("", "destId"); ctx.Points[destID] != nil && ctx.Points[destID].AttrValue("", "type") == "pres" {
				ctx.DiagramPresID = destID
				break
			}
		}
	}
	if ctx.DocID == "" || ctx.DiagramPresID == "" {
		return nil, "diagram root nodes are missing"
	}
	return ctx, ""
}

func smartArtResizeGroupFromRootCxn(ctx *smartArtResizeContext, root *ooxml.Node, childCxns []*ooxml.Node) smartArtResizeGroup {
	group := smartArtResizeGroup{RootCxn: root, RootSibTransID: root.AttrValue("", "sibTransId")}
	group.Root = smartArtResizeUnitFromCxn(ctx, root)
	for _, cxn := range append([]*ooxml.Node{root}, childCxns...) {
		contentID := cxn.AttrValue("", "destId")
		group.ContentIDs = append(group.ContentIDs, contentID)
		group.PointIDs = append(group.PointIDs, contentID, cxn.AttrValue("", "parTransId"), cxn.AttrValue("", "sibTransId"))
		group.CxnIDs = append(group.CxnIDs, cxn.AttrValue("", "modelId"))
	}
	for _, child := range childCxns {
		group.Children = append(group.Children, smartArtResizeUnitFromCxn(ctx, child))
	}
	group.PointIDs = smartArtUniqueIDs(group.PointIDs)
	group.ContentIDs = smartArtUniqueIDs(group.ContentIDs)
	group.PresIDs, group.TransitionPresIDs = smartArtPresIDsForGroup(ctx, group)
	group.CxnIDs = append(group.CxnIDs, smartArtCxnIDsForGroup(ctx, group.PresIDs, group.PointIDs)...)
	group.CxnIDs = smartArtUniqueIDs(group.CxnIDs)
	group.TransitionCxnIDs = smartArtCxnIDsForGroup(ctx, group.TransitionPresIDs, []string{group.RootSibTransID})
	return group
}

func smartArtResizeUnitFromCxn(ctx *smartArtResizeContext, cxn *ooxml.Node) smartArtResizeUnit {
	unit := smartArtResizeUnit{
		ContentID:  cxn.AttrValue("", "destId"),
		NormalCxn:  cxn,
		SibTransID: cxn.AttrValue("", "sibTransId"),
	}
	unit.PointIDs = smartArtUniqueIDs([]string{unit.ContentID, cxn.AttrValue("", "parTransId"), cxn.AttrValue("", "sibTransId")})
	unit.CxnIDs = smartArtUniqueIDs([]string{cxn.AttrValue("", "modelId")})
	unit.PresIDs, unit.TransitionPresIDs = smartArtPresIDsForPointIDs(ctx, unit.PointIDs, unit.SibTransID)
	unit.CxnIDs = append(unit.CxnIDs, smartArtCxnIDsForGroup(ctx, unit.PresIDs, unit.PointIDs)...)
	unit.CxnIDs = smartArtUniqueIDs(unit.CxnIDs)
	unit.TransitionCxnIDs = smartArtCxnIDsForGroup(ctx, unit.TransitionPresIDs, []string{unit.SibTransID})
	return unit
}

func smartArtPresIDsForGroup(ctx *smartArtResizeContext, group smartArtResizeGroup) ([]string, []string) {
	return smartArtPresIDsForPointIDs(ctx, group.PointIDs, group.RootSibTransID)
}

func smartArtPresIDsForPointIDs(ctx *smartArtResizeContext, pointIDs []string, transitionSibTransID string) ([]string, []string) {
	starts := map[string]bool{}
	transitionStarts := map[string]bool{}
	for _, id := range pointIDs {
		for _, pres := range ctx.PresByAssoc[id] {
			presID := pres.AttrValue("", "modelId")
			starts[presID] = true
			if id == transitionSibTransID {
				transitionStarts[presID] = true
			}
		}
	}
	all := smartArtCollectPresSubtree(ctx, starts)
	transitions := smartArtCollectPresSubtree(ctx, transitionStarts)
	return smartArtSortedPointIDs(ctx.PtList, all), smartArtSortedPointIDs(ctx.PtList, transitions)
}

func smartArtCollectPresSubtree(ctx *smartArtResizeContext, starts map[string]bool) map[string]bool {
	result := map[string]bool{}
	var walk func(string)
	walk = func(id string) {
		if id == "" || result[id] || ctx.Points[id] == nil {
			return
		}
		result[id] = true
		for _, cxn := range ctx.PresParBySrc[id] {
			walk(cxn.AttrValue("", "destId"))
		}
	}
	for id := range starts {
		walk(id)
	}
	return result
}

func smartArtCxnIDsForGroup(ctx *smartArtResizeContext, presIDs, pointIDs []string) []string {
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
			if pointSet[cxn.AttrValue("", "srcId")] || presSet[cxn.AttrValue("", "destId")] {
				result = append(result, id)
			}
		case "presParOf":
			if presSet[cxn.AttrValue("", "srcId")] || presSet[cxn.AttrValue("", "destId")] {
				result = append(result, id)
			}
		}
	}
	return result
}
