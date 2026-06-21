// Copyright 2026 The OpenAgent Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0

package smartart

import (
	"fmt"
	"strings"

	"github.com/the-open-agent/office-tool-use/model"
	"github.com/the-open-agent/office-tool-use/ooxml"
)

func CheckSmartArts(report *model.CheckReport, planIndex int, slide *model.SlideLibraryItem, edits []model.SmartArtEdit) {
	for _, edit := range edits {
		smartArt, selector := findSmartArt(slide, edit)
		if smartArt == nil {
			if !edit.Optional {
				addCheck(report, "ERROR", model.CheckResult{
					"plan_slide": planIndex, "source_slide": slide.SlideIndex, "selector": selector,
					"message": "SmartArt target not found in slide library",
				})
			}
			continue
		}
		if !smartArt.Editable {
			addCheck(report, "ERROR", model.CheckResult{
				"plan_slide": planIndex, "source_slide": slide.SlideIndex, "smartart_id": smartArt.SmartArtID,
				"message": "SmartArt is not editable: " + smartArt.Reason,
			})
			continue
		}
		if edit.Resize {
			if !smartArt.Resizable {
				addCheck(report, "ERROR", model.CheckResult{
					"plan_slide": planIndex, "source_slide": slide.SlideIndex, "smartart_id": smartArt.SmartArtID,
					"message": "SmartArt node count is not editable: " + smartArt.ResizeReason,
				})
				continue
			}
			if len(edit.Nodes) == 0 || len(edit.Nodes) > 20 {
				addCheck(report, "ERROR", model.CheckResult{
					"plan_slide": planIndex, "source_slide": slide.SlideIndex, "smartart_id": smartArt.SmartArtID,
					"message": "SmartArt resize needs 1 to 20 nodes",
				})
				continue
			}
			countChanges := len(edit.Nodes) != len(smartArt.Nodes)
			for index, node := range edit.Nodes {
				if countChanges && node.NodeID != "" {
					addCheck(report, "ERROR", model.CheckResult{
						"plan_slide": planIndex, "source_slide": slide.SlideIndex, "smartart_id": smartArt.SmartArtID,
						"node_id": node.NodeID, "message": "SmartArt resize with node count changes must use array order",
					})
					continue
				}
				if node.NodeID != "" && smartArtNodeByEdit(smartArt, node, index) == nil {
					addCheck(report, "ERROR", model.CheckResult{
						"plan_slide": planIndex, "source_slide": slide.SlideIndex, "smartart_id": smartArt.SmartArtID,
						"node_id": node.NodeID, "message": "SmartArt node target not found",
					})
					continue
				}
				addCheck(report, "OK", model.CheckResult{
					"plan_slide": planIndex, "source_slide": slide.SlideIndex, "smartart_id": smartArt.SmartArtID,
					"message": "SmartArt resize node is valid",
				})
			}
			continue
		}
		for _, op := range edit.StructureOps {
			if smartArt.Structure == nil {
				if !op.Optional {
					addCheck(report, "ERROR", model.CheckResult{
						"plan_slide": planIndex, "source_slide": slide.SlideIndex, "smartart_id": smartArt.SmartArtID,
						"message": "SmartArt structure operation requires a resizable structured SmartArt",
					})
				}
				continue
			}
			if !smartArtStructureOpSupported(smartArt.Structure.Kind, op.Op) {
				if !op.Optional {
					addCheck(report, "ERROR", model.CheckResult{
						"plan_slide": planIndex, "source_slide": slide.SlideIndex, "smartart_id": smartArt.SmartArtID,
						"message": "Unsupported SmartArt structure operation for " + smartArt.Structure.Kind + ": " + op.Op,
					})
				}
				continue
			}
			switch op.Op {
			case "add_child":
				if smartArtStructureGroupByRootNodeID(smartArt.Structure, op.ParentNodeID) == nil {
					if !op.Optional {
						addCheck(report, "ERROR", model.CheckResult{
							"plan_slide": planIndex, "source_slide": slide.SlideIndex, "smartart_id": smartArt.SmartArtID,
							"node_id": op.ParentNodeID, "message": "SmartArt parent node target not found",
						})
					}
					continue
				}
			case "add_root":
			}
			addCheck(report, "OK", model.CheckResult{
				"plan_slide": planIndex, "source_slide": slide.SlideIndex, "smartart_id": smartArt.SmartArtID,
				"message": "SmartArt structure operation is valid",
			})
		}
		for index, node := range edit.Nodes {
			target := smartArtNodeByEdit(smartArt, node, index)
			if target == nil {
				if !node.Optional {
					addCheck(report, "ERROR", model.CheckResult{
						"plan_slide": planIndex, "source_slide": slide.SlideIndex, "smartart_id": smartArt.SmartArtID,
						"node_id": node.NodeID, "message": "SmartArt node target not found",
					})
				}
				continue
			}
			addCheck(report, "OK", model.CheckResult{
				"plan_slide": planIndex, "source_slide": slide.SlideIndex, "smartart_id": smartArt.SmartArtID,
				"node_id": target.NodeID, "message": "SmartArt node target is valid",
			})
		}
	}
}

func smartArtStructureGroupByRootNodeID(info *model.SmartArtStructureInfo, nodeID string) *model.SmartArtStructureGroupInfo {
	if info == nil || nodeID == "" {
		return nil
	}
	for index := range info.Groups {
		if info.Groups[index].RootNodeID == nodeID {
			return &info.Groups[index]
		}
	}
	return nil
}

func findSmartArt(slide *model.SlideLibraryItem, edit model.SmartArtEdit) (*model.SmartArtInfo, string) {
	for _, selector := range []struct{ key, value string }{{"smartart_id", edit.SmartArtID}, {"shape_id", edit.ShapeID}, {"shape_name", edit.ShapeName}} {
		if selector.value == "" {
			continue
		}
		for index := range slide.SmartArts {
			item := &slide.SmartArts[index]
			if (selector.key == "smartart_id" && item.SmartArtID == selector.value) ||
				(selector.key == "shape_id" && item.ShapeID == selector.value) ||
				(selector.key == "shape_name" && item.ShapeName == selector.value) {
				return item, selector.key + ":" + selector.value
			}
		}
		return nil, selector.key + ":" + selector.value
	}
	return nil, ""
}

func smartArtNodeByEdit(info *model.SmartArtInfo, edit model.SmartArtNodeEdit, index int) *model.SmartArtNodeInfo {
	if edit.NodeID != "" {
		for nodeIndex := range info.Nodes {
			if info.Nodes[nodeIndex].NodeID == edit.NodeID {
				return &info.Nodes[nodeIndex]
			}
		}
		return nil
	}
	if index >= 0 && index < len(info.Nodes) {
		return &info.Nodes[index]
	}
	return nil
}

func ApplySmartArtEdits(pkg *ooxml.Package, slide *ooxml.Node, rels *ooxml.Relationships, types *ooxml.ContentTypes, sourceSlide int, slidePart string, edits []model.SmartArtEdit) error {
	if len(edits) == 0 {
		return nil
	}
	frames := smartArtFrames(slide)
	maps := map[string]*ooxml.Node{}
	for order, frame := range frames {
		id, name := ooxml.ShapeIdentity(frame, order+1)
		maps[fmt.Sprintf("smartart_id:s%02d_sa%s", sourceSlide, id)] = frame
		maps["shape_id:"+id] = frame
		if name != "" {
			maps["shape_name:"+name] = frame
		}
	}

	var missing []string
	for _, edit := range edits {
		selectors := smartArtSelectors(edit)
		var frame *ooxml.Node
		for _, selector := range selectors {
			if frame == nil {
				frame = maps[selector]
			}
		}
		if frame == nil {
			if !edit.Optional {
				missing = append(missing, selectorLabel(selectors))
			}
			continue
		}
		if err := applySmartArtEdit(pkg, frame, rels, types, sourceSlide, slidePart, edit); err != nil {
			return err
		}
	}
	if len(missing) != 0 {
		return fmt.Errorf("missing SmartArt target(s) on slide %d: %s", sourceSlide, strings.Join(missing, "; "))
	}
	return nil
}

func applySmartArtEdit(pkg *ooxml.Package, frame *ooxml.Node, rels *ooxml.Relationships, types *ooxml.ContentTypes, sourceSlide int, slidePart string, edit model.SmartArtEdit) error {
	shapeID, _ := ooxml.ShapeIdentity(frame, 0)
	relIDs := smartArtRelIDs(frame)
	if relIDs == nil {
		return fmt.Errorf("SmartArt %s on slide %d is missing diagram relationship IDs", shapeID, sourceSlide)
	}
	dataPart, err := relatedPartByID(slidePart, rels, relIDs.AttrValue(ooxml.NSOfficeRels, "dm"), ooxml.RelationshipTypeDiagramData)
	if err != nil {
		return fmt.Errorf("SmartArt %s on slide %d: %w", shapeID, sourceSlide, err)
	}
	dataRoot, err := pkg.XMLPart(dataPart)
	if err != nil {
		return err
	}
	if edit.Resize {
		model, reason := smartArtResizeModelFromData(dataRoot)
		if model == nil {
			return fmt.Errorf("SmartArt %s on slide %d cannot resize nodes: %s", shapeID, sourceSlide, reason)
		}
		if len(edit.Nodes) != len(model.Nodes) {
			return applySmartArtResizeEdit(pkg, rels, types, sourceSlide, slidePart, shapeID, dataPart, dataRoot, edit)
		}
	}
	if len(edit.StructureOps) != 0 {
		if err := applySmartArtStructureOps(pkg, rels, types, sourceSlide, slidePart, shapeID, dataPart, dataRoot, edit.StructureOps); err != nil {
			return err
		}
		if len(edit.Nodes) == 0 {
			return nil
		}
		dataRoot, err = pkg.XMLPart(dataPart)
		if err != nil {
			return err
		}
	}
	return applySmartArtNodeTextEdits(pkg, rels, sourceSlide, slidePart, shapeID, dataPart, dataRoot, edit.Nodes)
}

func applySmartArtNodeTextEdits(pkg *ooxml.Package, rels *ooxml.Relationships, sourceSlide int, slidePart, shapeID, dataPart string, dataRoot *ooxml.Node, nodes []model.SmartArtNodeEdit) error {
	drawingRoot, drawingPart, err := smartArtDrawingRoot(pkg, slidePart, rels, dataRoot)
	if err != nil {
		return fmt.Errorf("SmartArt %s on slide %d: %w", shapeID, sourceSlide, err)
	}
	nodeRefs := smartArtNodeRefs(dataRoot, drawingRoot)
	if len(nodeRefs) == 0 {
		return fmt.Errorf("SmartArt %s on slide %d has no editable nodes", shapeID, sourceSlide)
	}

	var missing []string
	for index, nodeEdit := range nodes {
		nodeIndex := index
		if nodeEdit.NodeID != "" {
			nodeIndex = smartArtNodeIndex(sourceSlide, shapeID, nodeEdit.NodeID, len(nodeRefs))
		}
		if nodeIndex < 0 || nodeIndex >= len(nodeRefs) {
			if !nodeEdit.Optional {
				missing = append(missing, nodeEdit.NodeID)
			}
			continue
		}
		text := smartArtEditText(nodeEdit)
		ref := nodeRefs[nodeIndex]
		if !setSmartArtDataText(dataRoot, ref.ModelID, text) {
			return fmt.Errorf("SmartArt %s on slide %d: node data not found: %s", shapeID, sourceSlide, ref.ModelID)
		}
		if drawingRoot != nil {
			for _, presID := range ref.PresIDs {
				setSmartArtDrawingText(drawingRoot, presID, text)
			}
		}
	}
	if len(missing) != 0 {
		return fmt.Errorf("missing SmartArt node target(s) on slide %d: %s", sourceSlide, strings.Join(missing, "; "))
	}

	data, err := ooxml.MarshalXML(dataRoot)
	if err != nil {
		return err
	}
	if err := pkg.SetPart(dataPart, data); err != nil {
		return err
	}
	if drawingRoot != nil {
		data, err := ooxml.MarshalXML(drawingRoot)
		if err != nil {
			return err
		}
		if err := pkg.SetPart(drawingPart, data); err != nil {
			return err
		}
	}
	return nil
}

func applySmartArtStructureOps(pkg *ooxml.Package, rels *ooxml.Relationships, types *ooxml.ContentTypes, sourceSlide int, slidePart, shapeID, dataPart string, dataRoot *ooxml.Node, ops []model.SmartArtStructureOp) error {
	model, reason := smartArtResizeModelFromData(dataRoot)
	if model == nil {
		return fmt.Errorf("SmartArt %s on slide %d cannot edit SmartArt structure: %s", shapeID, sourceSlide, reason)
	}
	drawingRoot, _, err := smartArtDrawingRoot(pkg, slidePart, rels, dataRoot)
	if err != nil {
		return fmt.Errorf("SmartArt %s on slide %d: %w", shapeID, sourceSlide, err)
	}
	nodeRefs := smartArtNodeRefs(dataRoot, drawingRoot)
	contentIDByNodeID := make(map[string]string, len(nodeRefs))
	for index, ref := range nodeRefs {
		contentIDByNodeID[fmt.Sprintf("s%02d_sa%s_n%02d", sourceSlide, shapeID, index+1)] = ref.ModelID
	}
	for _, op := range ops {
		var newContentID string
		var err error
		if !smartArtStructureOpSupported(model.Mode, op.Op) {
			if op.Optional {
				continue
			}
			return fmt.Errorf("SmartArt %s on slide %d: unsupported SmartArt structure operation for %s: %s", shapeID, sourceSlide, model.Mode, op.Op)
		}
		switch op.Op {
		case "add_child":
			parentContentID := contentIDByNodeID[op.ParentNodeID]
			if parentContentID == "" {
				if op.Optional {
					continue
				}
				return fmt.Errorf("SmartArt %s on slide %d: parent node target not found: %s", shapeID, sourceSlide, op.ParentNodeID)
			}
			newContentID, err = appendSmartArtChildToParent(dataRoot, model, parentContentID)
		case "add_root":
			newContentID, err = appendSmartArtRootOnly(dataRoot, model)
		}
		if err != nil {
			if op.Optional {
				continue
			}
			return fmt.Errorf("SmartArt %s on slide %d: %w", shapeID, sourceSlide, err)
		}
		if newContentID != "" && !setSmartArtDataText(dataRoot, newContentID, smartArtStructureOpText(op)) {
			return fmt.Errorf("SmartArt %s on slide %d: node data not found: %s", shapeID, sourceSlide, newContentID)
		}
		model, reason = smartArtResizeModelFromData(dataRoot)
		if model == nil {
			return fmt.Errorf("SmartArt %s on slide %d cannot edit SmartArt structure: %s", shapeID, sourceSlide, reason)
		}
	}
	if err := removeSmartArtDrawingCache(pkg, rels, types, slidePart, dataRoot); err != nil {
		return fmt.Errorf("SmartArt %s on slide %d: %w", shapeID, sourceSlide, err)
	}
	data, err := ooxml.MarshalXML(dataRoot)
	if err != nil {
		return err
	}
	return pkg.SetPart(dataPart, data)
}

func applySmartArtResizeEdit(pkg *ooxml.Package, rels *ooxml.Relationships, types *ooxml.ContentTypes, sourceSlide int, slidePart, shapeID, dataPart string, dataRoot *ooxml.Node, edit model.SmartArtEdit) error {
	if len(edit.Nodes) == 0 || len(edit.Nodes) > 20 {
		return fmt.Errorf("SmartArt %s on slide %d resize needs 1 to 20 nodes", shapeID, sourceSlide)
	}
	for _, node := range edit.Nodes {
		if node.NodeID != "" {
			return fmt.Errorf("SmartArt %s on slide %d resize with node count changes must use array order", shapeID, sourceSlide)
		}
	}
	model, reason := smartArtResizeModelFromData(dataRoot)
	if model == nil {
		return fmt.Errorf("SmartArt %s on slide %d cannot resize nodes: %s", shapeID, sourceSlide, reason)
	}
	resizeDelta := len(edit.Nodes) - len(model.Nodes)
	if model.ResizeStep <= 0 || len(edit.Nodes) < model.FixedNodes || (resizeDelta != 0 && absInt(resizeDelta)%model.ResizeStep != 0) {
		return fmt.Errorf("SmartArt %s on slide %d resize needs a complete %s node group", shapeID, sourceSlide, model.Mode)
	}
	for len(model.Nodes) < len(edit.Nodes) {
		if err := appendSmartArtResizeGroup(dataRoot, model); err != nil {
			return fmt.Errorf("SmartArt %s on slide %d: %w", shapeID, sourceSlide, err)
		}
		model, reason = smartArtResizeModelFromData(dataRoot)
		if model == nil {
			return fmt.Errorf("SmartArt %s on slide %d cannot resize nodes: %s", shapeID, sourceSlide, reason)
		}
	}
	for len(model.Nodes) > len(edit.Nodes) {
		if err := deleteSmartArtResizeTailGroup(dataRoot, model); err != nil {
			return fmt.Errorf("SmartArt %s on slide %d: %w", shapeID, sourceSlide, err)
		}
		model, reason = smartArtResizeModelFromData(dataRoot)
		if model == nil {
			return fmt.Errorf("SmartArt %s on slide %d cannot resize nodes: %s", shapeID, sourceSlide, reason)
		}
	}
	for index, nodeEdit := range edit.Nodes {
		if !setSmartArtDataText(dataRoot, model.Nodes[index].ContentID, smartArtEditText(nodeEdit)) {
			return fmt.Errorf("SmartArt %s on slide %d: node data not found: %s", shapeID, sourceSlide, model.Nodes[index].ContentID)
		}
	}
	if err := removeSmartArtDrawingCache(pkg, rels, types, slidePart, dataRoot); err != nil {
		return fmt.Errorf("SmartArt %s on slide %d: %w", shapeID, sourceSlide, err)
	}
	data, err := ooxml.MarshalXML(dataRoot)
	if err != nil {
		return err
	}
	return pkg.SetPart(dataPart, data)
}
