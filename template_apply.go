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
	"strconv"
	"strings"

	"github.com/the-open-agent/office-tool-use/model"
	"github.com/the-open-agent/office-tool-use/ooxml"
	"github.com/the-open-agent/office-tool-use/smartart"
)

func FillFile(input, output string, plan *model.Plan, options model.ApplyOptions, limits ooxml.Limits) (*model.CheckReport, error) {
	pkg, err := ooxml.OpenFile(input, limits)
	if err != nil {
		return nil, err
	}
	library := options.Library
	if library == nil {
		library, err = Analyze(pkg, input)
		if err != nil {
			return nil, err
		}
	}
	options.Library = library
	result, report, err := ApplyPlan(pkg, plan, options)
	if err != nil {
		return report, err
	}
	if err := result.WriteFileAtomic(output); err != nil {
		return report, err
	}
	return report, nil
}

func ApplyPlan(pkg *ooxml.Package, plan *model.Plan, options model.ApplyOptions) (*ooxml.Package, *model.CheckReport, error) {
	if pkg == nil || plan == nil {
		return nil, nil, fmt.Errorf("package and plan are required")
	}
	if plan.Schema != model.PlanSchema {
		return nil, nil, fmt.Errorf("plan schema must be %s", model.PlanSchema)
	}
	if len(plan.Slides) == 0 {
		return nil, nil, fmt.Errorf("plan must contain a non-empty slides list")
	}
	library := options.Library
	var err error
	if library == nil {
		library, err = Analyze(pkg, plan.SourcePPTX)
		if err != nil {
			return nil, nil, err
		}
	}
	report := CheckPlan(library, plan)
	if report.Summary.Error != 0 {
		return nil, report, fmt.Errorf("fill plan validation failed")
	}
	working := pkg.Clone()
	if options.Transition == "" {
		options.Transition = "keep"
	}
	if options.TransitionDuration == 0 {
		options.TransitionDuration = .5
	}
	if err := applyPlanUnchecked(working, plan, library, options); err != nil {
		return nil, report, err
	}
	if err := working.ValidatePresentation(); err != nil {
		return nil, report, err
	}
	return working, report, nil
}

func applyPlanUnchecked(pkg *ooxml.Package, plan *model.Plan, library *model.Library, options model.ApplyOptions) error {
	refs, err := pkg.SlideRefs()
	if err != nil {
		return err
	}
	refsByIndex := make(map[int]ooxml.SlideRef, len(refs))
	for _, ref := range refs {
		refsByIndex[ref.Index] = ref
	}
	libraryByIndex := make(map[int]*model.SlideLibraryItem, len(library.Slides))
	for index := range library.Slides {
		libraryByIndex[library.Slides[index].SlideIndex] = &library.Slides[index]
	}
	presentation, err := pkg.XMLPart("ppt/presentation.xml")
	if err != nil {
		return err
	}
	slideList := presentation.FirstDescendant(ooxml.NSPresentation, "sldIdLst")
	if slideList == nil {
		slideList = ooxml.Element(ooxml.NSPresentation, "sldIdLst")
		presentation.Children = append(presentation.Children, slideList)
	}
	maxSlideID := 255
	for _, item := range slideList.NamedChildren(ooxml.NSPresentation, "sldId") {
		if value, ok := ooxml.IntAttr(item, "", "id"); ok {
			maxSlideID = max(maxSlideID, value)
		}
	}
	slideList.RemoveChildren(ooxml.NSPresentation, "sldId")
	presentationRels, err := pkg.Relationships("ppt/presentation.xml")
	if err != nil {
		return err
	}
	presentationRels.RemoveByType(ooxml.RelationshipTypeSlide)
	types, err := pkg.ContentTypes()
	if err != nil {
		return err
	}
	allocator := ooxml.NewPartAllocator(pkg)
	nextSlide := nextNumberedPart(pkg, "ppt/slides", "slide", ".xml")
	nextChart := nextNumberedPart(pkg, "ppt/charts", "chart", ".xml")
	nextEmbedding := nextNumberedPart(pkg, "ppt/embeddings", "templateFillChart", ".xlsx")
	nextSlideID := maxSlideID + 1

	for offset, item := range plan.Slides {
		ref, ok := refsByIndex[item.SourceSlide]
		if !ok {
			return fmt.Errorf("plan references a missing source slide: %d", item.SourceSlide)
		}
		metadata := libraryByIndex[item.SourceSlide]
		if metadata == nil {
			return fmt.Errorf("slide %d is missing from slide library", item.SourceSlide)
		}
		slide, err := pkg.XMLPart(ref.PartName)
		if err != nil {
			return err
		}
		if err := applyReplacements(slide, item.SourceSlide, item.Replacements, metadata.Slots); err != nil {
			return err
		}
		if err := applyTableEdits(slide, item.SourceSlide, item.TableEdits, metadata.Tables); err != nil {
			return err
		}
		effect, duration, advance, err := resolveTransition(item, options.Transition, options.TransitionDuration)
		if err != nil {
			return err
		}
		setSlideTransition(slide, effect, duration, advance)

		newSlideNumber := nextSlide + offset
		newSlidePart := fmt.Sprintf("ppt/slides/slide%d.xml", newSlideNumber)
		sourceRels, err := pkg.Relationships(ref.PartName)
		if err != nil {
			return err
		}
		slideRels := &ooxml.Relationships{Items: append([]ooxml.Relationship(nil), sourceRels.Items...)}
		if err := cloneSlidePrivateParts(pkg, slideRels, newSlidePart, types, allocator); err != nil {
			return err
		}
		if err := smartart.ApplySmartArtEdits(pkg, slide, slideRels, types, item.SourceSlide, newSlidePart, item.SmartArtEdits); err != nil {
			return err
		}
		if err := applyImageEdits(pkg, slide, slideRels, types, allocator, item.SourceSlide, newSlidePart, item.ImageEdits); err != nil {
			return err
		}
		if err := applyChartEdits(pkg, slide, slideRels, types, item.SourceSlide, newSlidePart, item.ChartEdits, &nextChart, &nextEmbedding); err != nil {
			return err
		}
		notes := item.Notes
		if notes == "" {
			notes = item.SpeakerNotes
		}
		if err := setSlideNotes(pkg, newSlidePart, newSlideNumber, slideRels, notes, types); err != nil {
			return err
		}
		slideData, err := ooxml.MarshalXML(slide)
		if err != nil {
			return err
		}
		if err := pkg.SetPart(newSlidePart, slideData); err != nil {
			return err
		}
		if err := pkg.SetRelationships(newSlidePart, slideRels); err != nil {
			return err
		}
		if err := types.EnsureOverride(newSlidePart, ooxml.ContentTypeSlide); err != nil {
			return err
		}
		relID := presentationRels.NextID()
		target, err := ooxml.RelativeTarget("ppt/presentation.xml", newSlidePart)
		if err != nil {
			return err
		}
		if err := presentationRels.Add(ooxml.Relationship{ID: relID, Type: ooxml.RelationshipTypeSlide, Target: target, Mode: ooxml.TargetInternal}); err != nil {
			return err
		}
		slideID := ooxml.Element(ooxml.NSPresentation, "sldId", ooxml.PlainAttr("id", strconv.Itoa(nextSlideID+offset)))
		slideID.SetAttr(ooxml.NSOfficeRels, "id", relID)
		slideList.Children = append(slideList.Children, slideID)
	}
	presentationData, err := ooxml.MarshalXML(presentation)
	if err != nil {
		return err
	}
	if err := pkg.SetPart("ppt/presentation.xml", presentationData); err != nil {
		return err
	}
	if err := pkg.SetRelationships("ppt/presentation.xml", presentationRels); err != nil {
		return err
	}
	if err := pkg.SetContentTypes(types); err != nil {
		return err
	}
	if err := pkg.PruneUnreachable(); err != nil {
		return err
	}
	return nil
}

func normalizedError(value error) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(value.Error())
}
