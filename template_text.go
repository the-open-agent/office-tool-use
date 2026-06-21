// Copyright 2026 The OpenAgent Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package office

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/the-open-agent/office-tool-use/model"
	"github.com/the-open-agent/office-tool-use/ooxml"
)

func applyReplacements(slide *ooxml.Node, sourceSlide int, replacements []model.Replacement, metadata []model.TextSlot) error {
	containers := ooxml.TextContainers(slide)
	maps := map[string]*ooxml.Node{}
	for order, container := range containers {
		id, name := ooxml.ShapeIdentity(container, order+1)
		maps[fmt.Sprintf("slot_id:s%02d_sh%s", sourceSlide, id)] = container
		maps["shape_id:"+id] = container
		if name != "" {
			maps["shape_name:"+name] = container
		}
	}
	slotMaps := map[string]*model.TextSlot{}
	for index := range metadata {
		slot := &metadata[index]
		slotMaps["slot_id:"+slot.SlotID] = slot
		slotMaps["shape_id:"+slot.ShapeID] = slot
		if slot.ShapeName != "" {
			slotMaps["shape_name:"+slot.ShapeName] = slot
		}
	}
	var missing []string
	for _, replacement := range replacements {
		selectors := replacementSelectors(replacement)
		var container *ooxml.Node
		var slot *model.TextSlot
		for _, selector := range selectors {
			if container == nil {
				container = maps[selector]
			}
			if slot == nil {
				slot = slotMaps[selector]
			}
		}
		if container == nil {
			if !replacement.Optional {
				missing = append(missing, selectorLabel(selectors))
			}
			continue
		}
		singleLine := isSingleLineTitle(container)
		if slot != nil {
			singleLine = slot.SingleLine
		}
		if err := setContainerText(container, replacementText(replacement), replacement.PreserveLineBreaks || !singleLine, singleLine && !replacement.PreserveLineBreaks, slot); err != nil {
			return err
		}
	}
	if len(missing) != 0 {
		return fmt.Errorf("missing replacement target(s) on slide %d: %s", sourceSlide, strings.Join(missing, "; "))
	}
	return nil
}

func applyTableEdits(slide *ooxml.Node, sourceSlide int, edits []model.TableEdit, metadata []model.TableInfo) error {
	containers := ooxml.TableContainers(slide)
	maps := map[string]*ooxml.Node{}
	for order, container := range containers {
		id, name := ooxml.ShapeIdentity(container, order+1)
		maps[fmt.Sprintf("table_id:s%02d_tbl%s", sourceSlide, id)] = container
		maps["shape_id:"+id] = container
		if name != "" {
			maps["shape_name:"+name] = container
		}
	}
	metaMaps := map[string]*model.TableInfo{}
	for index := range metadata {
		table := &metadata[index]
		metaMaps["table_id:"+table.TableID] = table
		metaMaps["shape_id:"+table.ShapeID] = table
		if table.ShapeName != "" {
			metaMaps["shape_name:"+table.ShapeName] = table
		}
	}
	var invalid []string
	for _, edit := range edits {
		selectors := tableSelectors(edit)
		var frame *ooxml.Node
		var tableMeta *model.TableInfo
		for _, selector := range selectors {
			if frame == nil {
				frame = maps[selector]
			}
			if tableMeta == nil {
				tableMeta = metaMaps[selector]
			}
		}
		if frame == nil {
			if !edit.Optional {
				invalid = append(invalid, selectorLabel(selectors))
			}
			continue
		}
		table := frame.FirstDescendant(ooxml.NSDrawingML, "tbl")
		rows := table.NamedChildren(ooxml.NSDrawingML, "tr")
		for _, cellEdit := range edit.Cells {
			if cellEdit.Row < 0 || cellEdit.Row >= len(rows) {
				invalid = append(invalid, fmt.Sprintf("%s row=%d", selectorLabel(selectors), cellEdit.Row))
				continue
			}
			cells := rows[cellEdit.Row].NamedChildren(ooxml.NSDrawingML, "tc")
			if cellEdit.Col < 0 || cellEdit.Col >= len(cells) {
				invalid = append(invalid, fmt.Sprintf("%s row=%d col=%d", selectorLabel(selectors), cellEdit.Row, cellEdit.Col))
				continue
			}
			var slot *model.TextSlot
			if tableMeta != nil {
				if cell := findTableCell(tableMeta, cellEdit.Row, cellEdit.Col); cell != nil {
					slot = &model.TextSlot{
						Role: "body_candidate", ParagraphCount: cell.ParagraphCount, Geometry: cell.Geometry,
						TextMetrics: cell.TextMetrics,
					}
				}
			}
			if err := setContainerText(cells[cellEdit.Col], tableCellText(cellEdit), true, false, slot); err != nil {
				return err
			}
		}
	}
	if len(invalid) != 0 {
		return fmt.Errorf("invalid table edit target(s) on slide %d: %s", sourceSlide, strings.Join(invalid, "; "))
	}
	return nil
}

func setContainerText(container *ooxml.Node, text string, preserveLineBreaks, singleLine bool, slot *model.TextSlot) error {
	if singleLine && !preserveLineBreaks {
		text = collapseTitleLines(text)
	}
	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		lines = []string{""}
	}
	body := textBody(container)
	if body == nil {
		return fmt.Errorf("matched shape does not contain a text body")
	}
	paragraphs := body.NamedChildren(ooxml.NSDrawingML, "p")
	if len(lines) > 1 {
		if len(paragraphs) == 0 {
			paragraphs = []*ooxml.Node{ooxml.Element(ooxml.NSDrawingML, "p")}
		}
		templates := make([]*ooxml.Node, len(paragraphs))
		for index, paragraph := range paragraphs {
			templates[index] = paragraph.Clone()
		}
		body.RemoveChildren(ooxml.NSDrawingML, "p")
		for index, line := range lines {
			template := templates[min(index, len(templates)-1)].Clone()
			setParagraphText(template, line)
			body.Children = append(body.Children, template)
		}
	} else {
		nodes := container.Descendants(ooxml.NSDrawingML, "t")
		if len(nodes) == 0 {
			paragraph := body.Child(ooxml.NSDrawingML, "p")
			if paragraph == nil {
				paragraph = ooxml.Element(ooxml.NSDrawingML, "p")
				body.Children = append(body.Children, paragraph)
			}
			nodes = ensureParagraphTextNodes(paragraph)
		}
		nodes[0].Text = text
		for _, node := range nodes[1:] {
			node.Text = ""
		}
	}
	setNormalAutofit(body, singleLine)
	if slot != nil {
		layout := estimateTextLayout(text, slot.Role, max(slot.ParagraphCount, 1), slot.Geometry, slot.TextMetrics, singleLine)
		if layout.Scale != nil && *layout.Scale > 0 {
			base := fallbackFontSize(slot.Role, slot.Geometry, max(slot.ParagraphCount, 1))
			if slot.TextMetrics.FontSizePX != nil && *slot.TextMetrics.FontSizePX > 0 {
				base = *slot.TextMetrics.FontSizePX
			}
			setExplicitFontScale(container, *layout.Scale, base)
		}
	}
	return nil
}

func textBody(container *ooxml.Node) *ooxml.Node {
	if body := container.FirstDescendant(ooxml.NSPresentation, "txBody"); body != nil {
		return body
	}
	return container.FirstDescendant(ooxml.NSDrawingML, "txBody")
}

func setParagraphText(paragraph *ooxml.Node, text string) {
	nodes := paragraph.Descendants(ooxml.NSDrawingML, "t")
	if len(nodes) == 0 {
		nodes = ensureParagraphTextNodes(paragraph)
	}
	nodes[0].Text = text
	for _, node := range nodes[1:] {
		node.Text = ""
	}
}

func ensureParagraphTextNodes(paragraph *ooxml.Node) []*ooxml.Node {
	run := paragraph.Child(ooxml.NSDrawingML, "r")
	if run == nil {
		run = ooxml.Element(ooxml.NSDrawingML, "r")
		insertBeforeParagraphEnd(paragraph, run)
	}
	text := run.Child(ooxml.NSDrawingML, "t")
	if text == nil {
		text = ooxml.Element(ooxml.NSDrawingML, "t")
		run.Children = append(run.Children, text)
	}
	return []*ooxml.Node{text}
}

func insertBeforeParagraphEnd(paragraph, child *ooxml.Node) {
	for index, current := range paragraph.Children {
		if current.Name.Space == ooxml.NSDrawingML && current.Name.Local == "endParaRPr" {
			paragraph.Children = append(paragraph.Children, nil)
			copy(paragraph.Children[index+1:], paragraph.Children[index:])
			paragraph.Children[index] = child
			return
		}
	}
	paragraph.Children = append(paragraph.Children, child)
}

func setNormalAutofit(body *ooxml.Node, singleLine bool) {
	properties := body.Child(ooxml.NSDrawingML, "bodyPr")
	if properties == nil {
		properties = ooxml.Element(ooxml.NSDrawingML, "bodyPr")
		body.Children = append([]*ooxml.Node{properties}, body.Children...)
	}
	for _, local := range []string{"noAutofit", "spAutoFit", "normAutofit"} {
		properties.RemoveChildren(ooxml.NSDrawingML, local)
	}
	properties.Children = append(properties.Children, ooxml.Element(ooxml.NSDrawingML, "normAutofit"))
	if singleLine {
		properties.SetAttr("", "wrap", "none")
	} else if properties.AttrValue("", "wrap") == "none" {
		properties.SetAttr("", "wrap", "square")
	}
}

func setExplicitFontScale(container *ooxml.Node, scale, baseFontPX float64) {
	fallback := max(int(math.Round(baseFontPX*72/96*100)), 1)
	for _, local := range []string{"rPr", "defRPr", "endParaRPr"} {
		for _, properties := range container.Descendants(ooxml.NSDrawingML, local) {
			properties.SetAttr("", "sz", scaledFontSize(properties.AttrValue("", "sz"), scale, fallback))
		}
	}
	for _, local := range []string{"r", "fld"} {
		for _, run := range container.Descendants(ooxml.NSDrawingML, local) {
			properties := run.Child(ooxml.NSDrawingML, "rPr")
			if properties == nil {
				properties = ooxml.Element(ooxml.NSDrawingML, "rPr")
				run.Children = append([]*ooxml.Node{properties}, run.Children...)
			}
			if properties.AttrValue("", "sz") == "" {
				properties.SetAttr("", "sz", scaledFontSize("", scale, fallback))
			}
		}
	}
	for _, paragraph := range container.Descendants(ooxml.NSDrawingML, "p") {
		properties := paragraph.Child(ooxml.NSDrawingML, "pPr")
		if properties == nil {
			properties = ooxml.Element(ooxml.NSDrawingML, "pPr")
			paragraph.Children = append([]*ooxml.Node{properties}, paragraph.Children...)
		}
		defaultRun := properties.Child(ooxml.NSDrawingML, "defRPr")
		if defaultRun == nil {
			defaultRun = ooxml.Element(ooxml.NSDrawingML, "defRPr")
			properties.Children = append(properties.Children, defaultRun)
		}
		if defaultRun.AttrValue("", "sz") == "" {
			defaultRun.SetAttr("", "sz", scaledFontSize("", scale, fallback))
		}
		endRun := paragraph.Child(ooxml.NSDrawingML, "endParaRPr")
		if endRun == nil {
			endRun = ooxml.Element(ooxml.NSDrawingML, "endParaRPr")
			paragraph.Children = append(paragraph.Children, endRun)
		}
		if endRun.AttrValue("", "sz") == "" {
			endRun.SetAttr("", "sz", scaledFontSize("", scale, fallback))
		}
	}
}

func scaledFontSize(raw string, scale float64, fallback int) string {
	size, err := strconv.Atoi(raw)
	if err != nil {
		size = fallback
	}
	return strconv.Itoa(max(int(math.Round(float64(size)*scale)), 1))
}

func isSingleLineTitle(container *ooxml.Node) bool {
	kind, _, placeholder := ooxml.PlaceholderKey(container)
	if placeholder && (kind == "title" || kind == "ctrTitle") {
		return true
	}
	_, name := ooxml.ShapeIdentity(container, 0)
	lower := strings.ToLower(name)
	if strings.Contains(lower, "title") || strings.Contains(name, "标题") {
		return len(container.Descendants(ooxml.NSDrawingML, "p")) <= 1
	}
	body := textBody(container)
	properties := body
	if body != nil {
		properties = body.Child(ooxml.NSDrawingML, "bodyPr")
	}
	return properties != nil && properties.AttrValue("", "wrap") == "none" && len(container.Descendants(ooxml.NSDrawingML, "p")) <= 1
}

func replacementSelectors(value model.Replacement) []string {
	var result []string
	if value.SlotID != "" {
		result = append(result, "slot_id:"+value.SlotID)
	}
	if value.ShapeID != "" {
		result = append(result, "shape_id:"+value.ShapeID)
	}
	if value.ShapeName != "" {
		result = append(result, "shape_name:"+value.ShapeName)
	}
	return result
}

func tableSelectors(value model.TableEdit) []string {
	var result []string
	if value.TableID != "" {
		result = append(result, "table_id:"+value.TableID)
	}
	if value.ShapeID != "" {
		result = append(result, "shape_id:"+value.ShapeID)
	}
	if value.ShapeName != "" {
		result = append(result, "shape_name:"+value.ShapeName)
	}
	return result
}

func chartSelectors(value model.ChartEdit) []string {
	var result []string
	if value.ChartID != "" {
		result = append(result, "chart_id:"+value.ChartID)
	}
	if value.ShapeID != "" {
		result = append(result, "shape_id:"+value.ShapeID)
	}
	if value.ShapeName != "" {
		result = append(result, "shape_name:"+value.ShapeName)
	}
	return result
}

func selectorLabel(selectors []string) string {
	if len(selectors) == 0 {
		return "<missing selector>"
	}
	return strings.Join(selectors, ", ")
}
