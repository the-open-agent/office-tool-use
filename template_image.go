// Copyright 2026 The OpenAgent Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0

package office

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"

	"github.com/the-open-agent/office-tool-use/model"
	"github.com/the-open-agent/office-tool-use/ooxml"
)

func pictureContainers(root *ooxml.Node) []*ooxml.Node {
	return root.Descendants(ooxml.NSPresentation, "pic")
}

func analyzeImages(
	pkg *ooxml.Package,
	slide *ooxml.Node,
	ref ooxml.SlideRef,
	objectByID map[string]*model.SlideObject,
) ([]model.ImageInfo, error) {
	rels, err := pkg.Relationships(ref.PartName)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]ooxml.Relationship, len(rels.Items))
	for _, rel := range rels.Items {
		byID[rel.ID] = rel
	}

	containers := pictureContainers(slide)
	result := make([]model.ImageInfo, 0, len(containers))

	for order, container := range containers {
		shapeID, shapeName := ooxml.ShapeIdentity(container, order+1)
		description := ""
		if cNvPr := container.FirstDescendant(ooxml.NSPresentation, "cNvPr"); cNvPr != nil {
			description = cNvPr.AttrValue("", "descr")
		}

		blip := firstContainerBlip(container)
		if blip == nil {
			continue
		}
		embedID := blip.AttrValue(ooxml.NSOfficeRels, "embed")
		if embedID == "" {
			continue
		}
		rel, ok := byID[embedID]
		if !ok || rel.Type != ooxml.RelationshipTypeImage || rel.Mode != ooxml.TargetInternal {
			continue
		}
		partName, resolveErr := ooxml.ResolveTarget(ref.PartName, rel.Target)
		if resolveErr != nil || !pkg.HasPart(partName) {
			continue
		}

		geometry := model.Geometry{}
		if obj := objectByID[shapeID]; obj != nil {
			geometry = obj.Geometry
		}

		result = append(result, model.ImageInfo{
			ImageID:     fmt.Sprintf("s%02d_img%s", ref.Index, shapeID),
			ShapeID:     shapeID,
			ShapeName:   shapeName,
			Description: description,
			Geometry:    geometry,
		})
	}
	return result, nil
}

func firstContainerBlip(pic *ooxml.Node) *ooxml.Node {
	blipFill := pic.Child(ooxml.NSPresentation, "blipFill")
	if blipFill == nil {
		return nil
	}
	return blipFill.Child(ooxml.NSDrawingML, "blip")
}

type imagePayload struct {
	Data        []byte
	Extension   string
	ContentType string
}

func readImagePayload(path string, limit int64) (imagePayload, error) {
	if path == "" {
		return imagePayload{}, fmt.Errorf("image path is empty")
	}
	info, err := os.Stat(path)
	if err != nil {
		return imagePayload{}, fmt.Errorf("image file not found: %w", err)
	}
	if info.IsDir() {
		return imagePayload{}, fmt.Errorf("image path is a directory")
	}
	if info.Size() > limit {
		return imagePayload{}, fmt.Errorf("image file exceeds the %d MB size limit", limit>>20)
	}

	file, err := os.Open(path)
	if err != nil {
		return imagePayload{}, fmt.Errorf("cannot read image file: %w", err)
	}
	defer file.Close()

	limited := io.LimitReader(file, limit+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return imagePayload{}, fmt.Errorf("cannot read image file: %w", err)
	}
	if int64(len(data)) > limit {
		return imagePayload{}, fmt.Errorf("image file exceeds the %d MB size limit", limit>>20)
	}
	if len(data) == 0 {
		return imagePayload{}, fmt.Errorf("image file is empty")
	}

	// image.DecodeConfig is used only to detect the format; dimensions are intentionally
	// ignored because image replacement preserves the template frame's geometry as-is.
	_, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return imagePayload{}, fmt.Errorf("cannot decode image: %w", err)
	}

	switch format {
	case "png":
		return imagePayload{Data: data, Extension: ".png", ContentType: ooxml.ContentTypeImagePNG}, nil
	case "jpeg":
		return imagePayload{Data: data, Extension: ".jpeg", ContentType: ooxml.ContentTypeImageJPEG}, nil
	default:
		return imagePayload{}, fmt.Errorf("unsupported image format %q; only PNG and JPEG are supported", format)
	}
}

type imageShape struct {
	Node *ooxml.Node
	Blip *ooxml.Node
}

func applyImageEdits(
	pkg *ooxml.Package,
	slide *ooxml.Node,
	rels *ooxml.Relationships,
	types *ooxml.ContentTypes,
	allocator *ooxml.PartAllocator,
	sourceSlide int,
	newSlidePart string,
	edits []model.ImageEdit,
) error {
	if len(edits) == 0 {
		return nil
	}

	targets := make(map[string]imageShape)
	for _, pic := range pictureContainers(slide) {
		blip := firstContainerBlip(pic)
		if blip == nil {
			continue
		}
		embedID := blip.AttrValue(ooxml.NSOfficeRels, "embed")
		if embedID == "" {
			continue
		}
		shapeID, _ := ooxml.ShapeIdentity(pic, 0)
		imageID := fmt.Sprintf("s%02d_img%s", sourceSlide, shapeID)
		targets[imageID] = imageShape{Node: pic, Blip: blip}
	}

	for _, edit := range edits {
		target, ok := targets[edit.ImageID]
		if !ok {
			return fmt.Errorf("image edit %s on slide %d: image target not found", edit.ImageID, sourceSlide)
		}

		payload, err := readImagePayload(edit.ImagePath, pkg.Limits().MaxPartSize)
		if err != nil {
			return fmt.Errorf("image edit %s on slide %d: %s", edit.ImageID, sourceSlide, err.Error())
		}

		mediaPart := allocator.NextNumbered("ppt/media", "templateFillImage", payload.Extension)

		if err := pkg.SetPart(mediaPart, payload.Data); err != nil {
			return err
		}
		if err := types.EnsureOverride(mediaPart, payload.ContentType); err != nil {
			return err
		}

		relID := rels.NextID()
		targetName, err := ooxml.RelativeTarget(newSlidePart, mediaPart)
		if err != nil {
			return err
		}
		if err := rels.Add(ooxml.Relationship{
			ID:     relID,
			Type:   ooxml.RelationshipTypeImage,
			Target: targetName,
			Mode:   ooxml.TargetInternal,
		}); err != nil {
			return err
		}

		target.Blip.SetAttr(ooxml.NSOfficeRels, "embed", relID)
	}

	return nil
}
