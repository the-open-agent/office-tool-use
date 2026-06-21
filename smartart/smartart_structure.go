// Copyright 2026 The OpenAgent Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0

package smartart

import "github.com/the-open-agent/office-tool-use/model"

func smartArtStructureInfo(resizeModel *smartArtResizeModel, nodes []model.SmartArtNodeInfo) *model.SmartArtStructureInfo {
	if resizeModel == nil {
		return nil
	}
	nodeIDByModelID := make(map[string]string, len(nodes))
	for _, node := range nodes {
		if node.ModelID != "" {
			nodeIDByModelID[node.ModelID] = node.NodeID
		}
	}
	info := &model.SmartArtStructureInfo{
		Kind:           resizeModel.Mode,
		ResizeStep:     resizeModel.ResizeStep,
		FixedNodeCount: resizeModel.FixedNodes,
		AppendBehavior: smartArtAppendBehavior(resizeModel.Mode),
	}
	switch resizeModel.Mode {
	case "top_level_tail":
		for index, node := range resizeModel.Nodes {
			nodeID := nodeIDByModelID[node.ContentID]
			if nodeID == "" {
				continue
			}
			info.Groups = append(info.Groups, model.SmartArtStructureGroupInfo{
				Index:      index,
				NodeIDs:    []string{nodeID},
				RootNodeID: nodeID,
			})
		}
	default:
		for index, group := range resizeModel.Groups {
			groupInfo := model.SmartArtStructureGroupInfo{Index: index}
			for _, contentID := range group.ContentIDs {
				if nodeID := nodeIDByModelID[contentID]; nodeID != "" {
					groupInfo.NodeIDs = append(groupInfo.NodeIDs, nodeID)
				}
			}
			if nodeID := nodeIDByModelID[group.Root.ContentID]; nodeID != "" {
				groupInfo.RootNodeID = nodeID
			}
			for _, child := range group.Children {
				if nodeID := nodeIDByModelID[child.ContentID]; nodeID != "" {
					groupInfo.ChildNodeIDs = append(groupInfo.ChildNodeIDs, nodeID)
				}
			}
			if len(groupInfo.NodeIDs) != 0 {
				info.Groups = append(info.Groups, groupInfo)
			}
		}
	}
	return info
}

func smartArtAppendBehavior(mode string) string {
	switch mode {
	case "top_level_tail":
		return "resize by changing the complete flat nodes array length by 1"
	case "list_flat_composite_tail":
		return "resize by changing the complete flat nodes array length by 1"
	case "list_group_tail":
		return "use structure_ops add_child to add only a child under a chosen parent, add_root to add only an empty parent, or resize the complete flat nodes array by 2 for the legacy combined tail behavior"
	case "list_single_root_tail":
		return "use structure_ops add_child to add only a child under the single root, add_root to add only an empty parent, or resize the complete flat nodes array by 2 for the legacy combined tail behavior"
	default:
		return ""
	}
}

func smartArtStructureOpSupported(kind, op string) bool {
	switch op {
	case "add_child", "add_root":
		return kind == "list_group_tail" || kind == "list_single_root_tail"
	default:
		return false
	}
}
