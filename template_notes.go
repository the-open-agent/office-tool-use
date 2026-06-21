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
	"encoding/xml"
	"regexp"
	"strings"

	"github.com/the-open-agent/office-tool-use/ooxml"
)

var (
	markdownBoldA = regexp.MustCompile(`\*\*(.+?)\*\*`)
	markdownBoldB = regexp.MustCompile(`__(.+?)__`)
	markdownHead  = regexp.MustCompile(`^#+\s*`)
)

func markdownToPlainText(value string) string {
	stripBold := func(text string) string {
		text = markdownBoldA.ReplaceAllString(text, "$1")
		return markdownBoldB.ReplaceAllString(text, "$1")
	}
	var lines []string
	for _, raw := range strings.Split(value, "\n") {
		trimmed := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(raw, "#"):
			text := stripBold(strings.TrimSpace(markdownHead.ReplaceAllString(raw, "")))
			if text != "" {
				lines = append(lines, text, "")
			}
		case strings.HasPrefix(trimmed, "- "):
			lines = append(lines, "• "+stripBold(strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))))
		case trimmed != "":
			lines = append(lines, stripBold(trimmed))
		default:
			lines = append(lines, "")
		}
	}
	result := lines[:0]
	previousEmpty := false
	for _, line := range lines {
		if line != "" {
			result = append(result, line)
			previousEmpty = false
		} else if !previousEmpty {
			result = append(result, "")
			previousEmpty = true
		}
	}
	return strings.TrimSpace(strings.Join(result, "\n"))
}

func findNotesMasterPart(pkg *ooxml.Package) (string, bool) {
	if part, ok, err := pkg.RelatedPart("ppt/presentation.xml", ooxml.RelationshipTypeNotesMaster); err == nil && ok {
		return part, true
	}
	for _, name := range pkg.PartNames() {
		if !strings.HasPrefix(name, "ppt/notesSlides/notesSlide") || !strings.HasSuffix(name, ".xml") {
			continue
		}
		if part, ok, err := pkg.RelatedPart(name, ooxml.RelationshipTypeNotesMaster); err == nil && ok {
			return part, true
		}
	}
	return "", false
}

func setSlideNotes(pkg *ooxml.Package, slidePart string, slideNumber int, rels *ooxml.Relationships, notes string, types *ooxml.ContentTypes) error {
	rels.RemoveByType(ooxml.RelationshipTypeNotesSlide)
	notes = strings.TrimSpace(notes)
	if notes == "" {
		return nil
	}
	notesPart := "ppt/notesSlides/notesSlide" + itoa(slideNumber) + ".xml"
	root := createNotesXML(markdownToPlainText(notes))
	data, err := ooxml.MarshalXML(root)
	if err != nil {
		return err
	}
	if err := pkg.SetPart(notesPart, data); err != nil {
		return err
	}
	if err := types.EnsureOverride(notesPart, ooxml.ContentTypeNotesSlide); err != nil {
		return err
	}
	target, err := ooxml.RelativeTarget(slidePart, notesPart)
	if err != nil {
		return err
	}
	if err := rels.Add(ooxml.Relationship{ID: rels.NextID(), Type: ooxml.RelationshipTypeNotesSlide, Target: target, Mode: ooxml.TargetInternal}); err != nil {
		return err
	}
	notesRels := &ooxml.Relationships{}
	if master, ok := findNotesMasterPart(pkg); ok {
		masterTarget, err := ooxml.RelativeTarget(notesPart, master)
		if err != nil {
			return err
		}
		notesRels.Items = append(notesRels.Items, ooxml.Relationship{ID: notesRels.NextID(), Type: ooxml.RelationshipTypeNotesMaster, Target: masterTarget, Mode: ooxml.TargetInternal})
	}
	slideTarget, err := ooxml.RelativeTarget(notesPart, slidePart)
	if err != nil {
		return err
	}
	notesRels.Items = append(notesRels.Items, ooxml.Relationship{ID: notesRels.NextID(), Type: ooxml.RelationshipTypeSlide, Target: slideTarget, Mode: ooxml.TargetInternal})
	return pkg.SetRelationships(notesPart, notesRels)
}

func createNotesXML(notes string) *ooxml.Node {
	root := ooxml.Element(ooxml.NSPresentation, "notes")
	common := ooxml.Element(ooxml.NSPresentation, "cSld")
	tree := ooxml.Element(ooxml.NSPresentation, "spTree")
	group := ooxml.Element(ooxml.NSPresentation, "nvGrpSpPr")
	group.Children = []*ooxml.Node{
		ooxml.Element(ooxml.NSPresentation, "cNvPr", ooxml.PlainAttr("id", "1"), ooxml.PlainAttr("name", "")),
		ooxml.Element(ooxml.NSPresentation, "cNvGrpSpPr"), ooxml.Element(ooxml.NSPresentation, "nvPr"),
	}
	groupProperties := ooxml.Element(ooxml.NSPresentation, "grpSpPr")
	transform := ooxml.Element(ooxml.NSDrawingML, "xfrm")
	transform.Children = []*ooxml.Node{
		ooxml.Element(ooxml.NSDrawingML, "off", ooxml.PlainAttr("x", "0"), ooxml.PlainAttr("y", "0")),
		ooxml.Element(ooxml.NSDrawingML, "ext", ooxml.PlainAttr("cx", "0"), ooxml.PlainAttr("cy", "0")),
		ooxml.Element(ooxml.NSDrawingML, "chOff", ooxml.PlainAttr("x", "0"), ooxml.PlainAttr("y", "0")),
		ooxml.Element(ooxml.NSDrawingML, "chExt", ooxml.PlainAttr("cx", "0"), ooxml.PlainAttr("cy", "0")),
	}
	groupProperties.Children = []*ooxml.Node{transform}
	tree.Children = append(tree.Children, group, groupProperties, slideImagePlaceholder(), notesPlaceholder())
	common.Children = []*ooxml.Node{tree}
	color := ooxml.Element(ooxml.NSPresentation, "clrMapOvr")
	color.Children = []*ooxml.Node{ooxml.Element(ooxml.NSDrawingML, "masterClrMapping")}
	root.Children = []*ooxml.Node{common, color}

	body := tree.Children[len(tree.Children)-1].Child(ooxml.NSPresentation, "txBody")
	for _, line := range strings.Split(notes, "\n") {
		paragraph := ooxml.Element(ooxml.NSDrawingML, "p")
		if strings.TrimSpace(line) == "" {
			paragraph.Children = []*ooxml.Node{ooxml.Element(ooxml.NSDrawingML, "endParaRPr", ooxml.PlainAttr("lang", "zh-CN"), ooxml.PlainAttr("dirty", "0"))}
		} else {
			run := ooxml.Element(ooxml.NSDrawingML, "r")
			run.Children = []*ooxml.Node{
				ooxml.Element(ooxml.NSDrawingML, "rPr", ooxml.PlainAttr("lang", "zh-CN"), ooxml.PlainAttr("dirty", "0")),
				{Name: xmlName(ooxml.NSDrawingML, "t"), Text: line},
			}
			paragraph.Children = []*ooxml.Node{run}
		}
		body.Children = append(body.Children, paragraph)
	}
	if notes == "" {
		body.Children = append(body.Children, ooxml.Element(ooxml.NSDrawingML, "p"))
	}
	return root
}

func slideImagePlaceholder() *ooxml.Node {
	shape := ooxml.Element(ooxml.NSPresentation, "sp")
	nonVisual := ooxml.Element(ooxml.NSPresentation, "nvSpPr")
	nonVisualProps := ooxml.Element(ooxml.NSPresentation, "cNvPr", ooxml.PlainAttr("id", "2"), ooxml.PlainAttr("name", "Slide Image Placeholder 1"))
	nonVisualShape := ooxml.Element(ooxml.NSPresentation, "cNvSpPr")
	locks := ooxml.Element(ooxml.NSDrawingML, "spLocks",
		ooxml.PlainAttr("noGrp", "1"),
		ooxml.PlainAttr("noRot", "1"),
		ooxml.PlainAttr("noChangeAspect", "1"),
	)
	nonVisualShape.Children = []*ooxml.Node{locks}
	app := ooxml.Element(ooxml.NSPresentation, "nvPr")
	app.Children = []*ooxml.Node{ooxml.Element(ooxml.NSPresentation, "ph", ooxml.PlainAttr("type", "sldImg"))}
	nonVisual.Children = []*ooxml.Node{nonVisualProps, nonVisualShape, app}
	shape.Children = []*ooxml.Node{nonVisual, ooxml.Element(ooxml.NSPresentation, "spPr")}
	return shape
}

func notesPlaceholder() *ooxml.Node {
	shape := ooxml.Element(ooxml.NSPresentation, "sp")
	nonVisual := ooxml.Element(ooxml.NSPresentation, "nvSpPr")
	nonVisualProps := ooxml.Element(ooxml.NSPresentation, "cNvPr", ooxml.PlainAttr("id", "3"), ooxml.PlainAttr("name", "Notes Placeholder 2"))
	nonVisualShape := ooxml.Element(ooxml.NSPresentation, "cNvSpPr")
	nonVisualShape.Children = []*ooxml.Node{ooxml.Element(ooxml.NSDrawingML, "spLocks", ooxml.PlainAttr("noGrp", "1"))}
	app := ooxml.Element(ooxml.NSPresentation, "nvPr")
	app.Children = []*ooxml.Node{ooxml.Element(ooxml.NSPresentation, "ph", ooxml.PlainAttr("type", "body"), ooxml.PlainAttr("idx", "1"))}
	nonVisual.Children = []*ooxml.Node{nonVisualProps, nonVisualShape, app}
	body := ooxml.Element(ooxml.NSPresentation, "txBody")
	body.Children = []*ooxml.Node{ooxml.Element(ooxml.NSDrawingML, "bodyPr"), ooxml.Element(ooxml.NSDrawingML, "lstStyle")}
	shape.Children = []*ooxml.Node{nonVisual, ooxml.Element(ooxml.NSPresentation, "spPr"), body}
	return shape
}

func xmlName(space, local string) xml.Name {
	return xml.Name{Space: space, Local: local}
}

func itoa(value int) string {
	const digits = "0123456789"
	if value == 0 {
		return "0"
	}
	var buffer [20]byte
	index := len(buffer)
	for value > 0 {
		index--
		buffer[index] = digits[value%10]
		value /= 10
	}
	return string(buffer[index:])
}
