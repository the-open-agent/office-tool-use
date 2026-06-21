// Copyright 2026 The OpenAgent Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0

package smartart

import (
	"strings"

	"github.com/the-open-agent/office-tool-use/model"
	"github.com/the-open-agent/office-tool-use/ooxml"
)

func addCheck(report *model.CheckReport, status string, result model.CheckResult) {
	result["status"] = status
	report.Results = append(report.Results, result)
	switch status {
	case "OK":
		report.Summary.OK++
	case "WARN":
		report.Summary.Warn++
	case "ERROR":
		report.Summary.Error++
	}
}

func selectorLabel(selectors []string) string {
	if len(selectors) == 0 {
		return "<no selector>"
	}
	return strings.Join(selectors, ", ")
}

func setParagraphText(paragraph *ooxml.Node, text string) {
	nodes := paragraph.Descendants(ooxml.NSDrawingML, "t")
	if len(nodes) == 0 {
		run := paragraph.Child(ooxml.NSDrawingML, "r")
		if run == nil {
			run = ooxml.Element(ooxml.NSDrawingML, "r")
			paragraph.Children = append(paragraph.Children, run)
		}
		node := run.Child(ooxml.NSDrawingML, "t")
		if node == nil {
			node = ooxml.Element(ooxml.NSDrawingML, "t")
			run.Children = append(run.Children, node)
		}
		nodes = []*ooxml.Node{node}
	}
	for index, node := range nodes {
		if index == 0 {
			node.Text = text
		} else {
			node.Text = ""
		}
	}
}
