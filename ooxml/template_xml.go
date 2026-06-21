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

package ooxml

import (
	"fmt"
	"math"
	"path"
	"strconv"
	"strings"
	"unicode"

	"github.com/the-open-agent/office-tool-use/model"
)

const (
	emuPerInch = 914400.0
	pxPerInch  = 96.0
)

type SlideRef struct {
	Index    int
	RelID    string
	PartName string
	RelsName string
}

func (p *Package) SlideRefs() ([]SlideRef, error) {
	presentation, err := p.XMLPart("ppt/presentation.xml")
	if err != nil {
		return nil, err
	}
	rels, err := p.Relationships("ppt/presentation.xml")
	if err != nil {
		return nil, err
	}
	byID := make(map[string]Relationship, len(rels.Items))
	for _, rel := range rels.Items {
		byID[rel.ID] = rel
	}
	list := presentation.FirstDescendant(nsPresentation, "sldIdLst")
	if list == nil {
		return []SlideRef{}, nil
	}
	var result []SlideRef
	for _, item := range list.NamedChildren(nsPresentation, "sldId") {
		relID := item.AttrValue(nsOfficeRels, "id")
		rel, ok := byID[relID]
		if !ok || rel.Type != RelationshipTypeSlide || rel.Mode != TargetInternal {
			continue
		}
		partName, err := ResolveTarget("ppt/presentation.xml", rel.Target)
		if err != nil {
			return nil, err
		}
		relsName, err := RelationshipsPart(partName)
		if err != nil {
			return nil, err
		}
		result = append(result, SlideRef{Index: len(result) + 1, RelID: relID, PartName: partName, RelsName: relsName})
	}
	return result, nil
}

func (p *Package) XMLPart(name string) (*Node, error) {
	data, err := p.ReadPart(name)
	if err != nil {
		return nil, err
	}
	root, err := ParseXML(data)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", name, err)
	}
	return root, nil
}

func EMUToPX(raw string) *int {
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return nil
	}
	result := int(math.Round(float64(value) / emuPerInch * pxPerInch))
	return &result
}

func ShapeIdentity(container *Node, order int) (string, string) {
	property := container.FirstDescendant(nsPresentation, "cNvPr")
	if property == nil {
		return strconv.Itoa(order), ""
	}
	id := property.AttrValue("", "id")
	if id == "" {
		id = strconv.Itoa(order)
	}
	return id, property.AttrValue("", "name")
}

func TextContainers(root *Node) []*Node {
	var result []*Node
	for _, local := range []string{"sp", "graphicFrame"} {
		for _, candidate := range root.Descendants(nsPresentation, local) {
			if candidate.FirstDescendant(nsPresentation, "txBody") != nil ||
				candidate.FirstDescendant(nsDrawingML, "txBody") != nil ||
				candidate.FirstDescendant(nsDrawingML, "t") != nil {
				result = append(result, candidate)
			}
		}
	}
	return result
}

func TableContainers(root *Node) []*Node {
	var result []*Node
	for _, candidate := range root.Descendants(nsPresentation, "graphicFrame") {
		if candidate.FirstDescendant(nsDrawingML, "tbl") != nil {
			result = append(result, candidate)
		}
	}
	return result
}

func ChartContainers(root *Node) []*Node {
	var result []*Node
	for _, candidate := range root.Descendants(nsPresentation, "graphicFrame") {
		if candidate.FirstDescendant(nsChart, "chart") != nil {
			result = append(result, candidate)
		}
	}
	return result
}

func ParagraphTexts(container *Node) []string {
	var paragraphs []string
	for _, paragraph := range container.Descendants(nsDrawingML, "p") {
		var builder strings.Builder
		for _, text := range paragraph.Descendants(nsDrawingML, "t") {
			builder.WriteString(TextContent(text))
		}
		value := strings.TrimSpace(builder.String())
		if value != "" {
			paragraphs = append(paragraphs, value)
		}
	}
	if len(paragraphs) != 0 {
		return paragraphs
	}
	var builder strings.Builder
	for _, text := range container.Descendants(nsDrawingML, "t") {
		builder.WriteString(TextContent(text))
	}
	value := strings.TrimSpace(builder.String())
	if value != "" {
		return []string{value}
	}
	return []string{}
}

func ContainerGeometry(container *Node) model.Geometry {
	var transform *Node
	if properties := container.Child(nsPresentation, "spPr"); properties != nil {
		transform = properties.Child(nsDrawingML, "xfrm")
	}
	if transform == nil {
		transform = container.Child(nsPresentation, "xfrm")
	}
	if transform == nil {
		transform = container.FirstDescendant(nsDrawingML, "xfrm")
	}
	if transform == nil {
		return model.Geometry{}
	}
	offset := transform.Child(nsDrawingML, "off")
	extent := transform.Child(nsDrawingML, "ext")
	var result model.Geometry
	if offset != nil {
		result.X = EMUToPX(offset.AttrValue("", "x"))
		result.Y = EMUToPX(offset.AttrValue("", "y"))
	}
	if extent != nil {
		result.Width = EMUToPX(extent.AttrValue("", "cx"))
		result.Height = EMUToPX(extent.AttrValue("", "cy"))
	}
	return result
}

func PlaceholderKey(container *Node) (string, string, bool) {
	placeholder := container.FirstDescendant(nsPresentation, "ph")
	if placeholder == nil {
		return "", "", false
	}
	kind := placeholder.AttrValue("", "type")
	if kind == "" {
		kind = "body"
	}
	return kind, placeholder.AttrValue("", "idx"), true
}

func PlaceholderMap(root *Node) map[string]*Node {
	result := map[string]*Node{}
	if root == nil {
		return result
	}
	for _, container := range root.Descendants(nsPresentation, "sp") {
		kind, index, ok := PlaceholderKey(container)
		if ok {
			result[kind+"\x00"+index] = container
		}
	}
	return result
}

func InheritedChain(container, layout, master *Node) []*Node {
	result := []*Node{container}
	kind, index, ok := PlaceholderKey(container)
	if !ok {
		return result
	}
	key := kind + "\x00" + index
	if inherited := PlaceholderMap(layout)[key]; inherited != nil {
		result = append(result, inherited)
	}
	if inherited := PlaceholderMap(master)[key]; inherited != nil {
		result = append(result, inherited)
	}
	return result
}

func (p *Package) RelatedPart(owner, relType string) (string, bool, error) {
	rels, err := p.Relationships(owner)
	if err != nil {
		return "", false, err
	}
	for _, rel := range rels.Items {
		if rel.Type != relType || rel.Mode != TargetInternal {
			continue
		}
		target, err := ResolveTarget(owner, rel.Target)
		return target, err == nil, err
	}
	return "", false, nil
}

func (p *Package) InheritanceRoots(slidePart string) (*Node, *Node, error) {
	layoutPart, ok, err := p.RelatedPart(slidePart, RelationshipTypeSlideLayout)
	if err != nil || !ok {
		return nil, nil, err
	}
	layout, err := p.XMLPart(layoutPart)
	if err != nil {
		return nil, nil, err
	}
	masterPart, ok, err := p.RelatedPart(layoutPart, RelationshipTypeSlideMaster)
	if err != nil || !ok {
		return layout, nil, err
	}
	master, err := p.XMLPart(masterPart)
	return layout, master, err
}

func TextMetricsForChain(chain []*Node, paragraphCount int) model.TextMetrics {
	result := model.TextMetrics{
		Paragraphs: paragraphCount,
		Wrap:       "square",
		Autofit:    "none",
		MarginsPX:  map[string]int{},
		Anchor:     "t",
		Alignment:  "l",
	}
	var sizes []float64
	for _, container := range chain {
		for _, local := range []string{"rPr", "defRPr"} {
			for _, properties := range container.Descendants(nsDrawingML, local) {
				if value, ok := FloatAttr(properties, "", "sz"); ok {
					sizes = append(sizes, value/100*96/72)
				}
			}
		}
		if len(sizes) != 0 {
			break
		}
	}
	for _, size := range sizes {
		if result.FontSizePX == nil || size > *result.FontSizePX {
			value := math.Round(size*100) / 100
			result.FontSizePX = &value
		}
	}
	for _, container := range chain {
		body := container.FirstDescendant(nsDrawingML, "bodyPr")
		if body == nil {
			continue
		}
		if value := body.AttrValue("", "wrap"); value != "" {
			result.Wrap = value
		}
		if value := body.AttrValue("", "anchor"); value != "" {
			result.Anchor = value
		}
		if body.Child(nsDrawingML, "normAutofit") != nil {
			result.Autofit = "normal"
		} else if body.Child(nsDrawingML, "spAutoFit") != nil {
			result.Autofit = "shape"
		} else if body.Child(nsDrawingML, "noAutofit") != nil {
			result.Autofit = "none"
		}
		for key, attribute := range map[string]string{"left": "lIns", "right": "rIns", "top": "tIns", "bottom": "bIns"} {
			if px := EMUToPX(body.AttrValue("", attribute)); px != nil {
				result.MarginsPX[key] = *px
			}
		}
		break
	}
	for _, container := range chain {
		for _, paragraph := range container.Descendants(nsDrawingML, "pPr") {
			if alignment := paragraph.AttrValue("", "algn"); alignment != "" {
				result.Alignment = alignment
			}
			if spacing := paragraph.Child(nsDrawingML, "lnSpc"); spacing != nil {
				if percent := spacing.Child(nsDrawingML, "spcPct"); percent != nil {
					if value, ok := FloatAttr(percent, "", "val"); ok {
						value /= 100000
						result.LineSpacing = &value
					}
				}
				if points := spacing.Child(nsDrawingML, "spcPts"); points != nil {
					if value, ok := FloatAttr(points, "", "val"); ok {
						value = value / 100 * 96 / 72
						result.LineSpacePX = &value
					}
				}
			}
			return result
		}
	}
	return result
}

func SlotRole(text, name, placeholder string, metrics model.TextMetrics, geometry model.Geometry, paragraphs, textNodes int) string {
	lowerName := strings.ToLower(name)
	if placeholder == "title" || placeholder == "ctrTitle" || strings.Contains(lowerName, "title") || strings.Contains(name, "标题") {
		return "title_candidate"
	}
	if metrics.FontSizePX != nil && *metrics.FontSizePX >= 30 && geometry.Y != nil && *geometry.Y < 100 &&
		geometry.Width != nil && *geometry.Width >= 300 {
		return "title_candidate"
	}
	if placeholder == "body" || placeholder == "subTitle" || placeholder == "obj" {
		return "body_candidate"
	}
	if paragraphs > 1 || (metrics.Wrap != "none" && (len([]rune(text)) >= 36 || textNodes >= 3)) || len([]rune(text)) >= 72 {
		return "body_candidate"
	}
	return "label_candidate"
}

func VisualWidth(value string) float64 {
	var width float64
	for _, char := range value {
		if unicode.IsSpace(char) {
			continue
		}
		if char >= 0x1100 && (char <= 0x115f || char >= 0x2e80) {
			width += 2
		} else {
			width++
		}
	}
	return width
}

func PtrInt(value int) *int { return &value }

func TruncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) > limit {
		runes = runes[:limit]
	}
	return string(runes)
}

func PartBaseNumber(name, directory, prefix, suffix string) int {
	if path.Dir(name) != directory {
		return 0
	}
	base := path.Base(name)
	if !strings.HasPrefix(base, prefix) || !strings.HasSuffix(base, suffix) {
		return 0
	}
	value, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(base, prefix), suffix))
	return value
}

const (
	EMUPerInch = emuPerInch
	PXPerInch  = pxPerInch
)
