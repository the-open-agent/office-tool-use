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
	"path"
	"strings"

	"github.com/the-open-agent/office-tool-use/ooxml"
)

var sharedRelationshipTypes = map[string]bool{
	ooxml.RelationshipTypeSlideLayout: true,
	ooxml.RelationshipTypeSlideMaster: true,
	ooxml.RelationshipTypeNotesMaster: true,
	ooxml.RelationshipTypeTheme:       true,
	"http://schemas.openxmlformats.org/officeDocument/2006/relationships/presProps":   true,
	"http://schemas.openxmlformats.org/officeDocument/2006/relationships/viewProps":   true,
	"http://schemas.openxmlformats.org/officeDocument/2006/relationships/tableStyles": true,
}

func cloneSlidePrivateParts(pkg *ooxml.Package, rels *ooxml.Relationships, newSlidePart string, types *ooxml.ContentTypes, allocator *ooxml.PartAllocator) error {
	return clonePrivateRelationships(pkg, rels, newSlidePart, types, allocator, map[string]string{})
}

func clonePrivateRelationships(pkg *ooxml.Package, rels *ooxml.Relationships, owner string, types *ooxml.ContentTypes, allocator *ooxml.PartAllocator, cloned map[string]string) error {
	for index := range rels.Items {
		rel := &rels.Items[index]
		if rel.Mode == ooxml.TargetExternal || sharedRelationshipTypes[rel.Type] ||
			rel.Type == ooxml.RelationshipTypeChart || rel.Type == ooxml.RelationshipTypeNotesSlide || rel.Type == ooxml.RelationshipTypeSlide {
			continue
		}
		sourcePart, err := ooxml.ResolveTarget(owner, rel.Target)
		if err != nil || !pkg.HasPart(sourcePart) {
			continue
		}
		contentType, explicit := explicitContentType(types, sourcePart)
		if !explicit {
			continue
		}
		newPart := cloned[sourcePart]
		if newPart == "" {
			newPart = allocator.NextSibling(sourcePart, "_tf")
			data, err := pkg.ReadPart(sourcePart)
			if err != nil {
				return err
			}
			if err := pkg.SetPart(newPart, data); err != nil {
				return err
			}
			if err := types.EnsureOverride(newPart, contentType); err != nil {
				return err
			}
			cloned[sourcePart] = newPart
			sourceRels, err := pkg.Relationships(sourcePart)
			if err != nil {
				return err
			}
			if len(sourceRels.Items) != 0 {
				subRels := &ooxml.Relationships{Items: append([]ooxml.Relationship(nil), sourceRels.Items...)}
				if err := clonePrivateRelationships(pkg, subRels, newPart, types, allocator, cloned); err != nil {
					return err
				}
				if err := pkg.SetRelationships(newPart, subRels); err != nil {
					return err
				}
			}
		}
		target, err := ooxml.RelativeTarget(owner, newPart)
		if err != nil {
			return err
		}
		rel.Target = target
	}
	return nil
}

func explicitContentType(types *ooxml.ContentTypes, partName string) (string, bool) {
	normalized, err := ooxml.NormalizePartName(partName)
	if err != nil {
		return "", false
	}
	for _, override := range types.Overrides {
		item, err := ooxml.NormalizePartName(override.PartName)
		if err == nil && item == normalized {
			return override.ContentType, true
		}
	}
	return "", false
}

func nextNumberedPart(pkg *ooxml.Package, directory, prefix, suffix string) int {
	maxNumber := 0
	for _, name := range pkg.PartNames() {
		maxNumber = max(maxNumber, ooxml.PartBaseNumber(name, directory, prefix, suffix))
	}
	return maxNumber + 1
}

func nextSiblingName(pkg *ooxml.Package, source, marker string) string {
	directory := path.Dir(source)
	extension := path.Ext(source)
	stem := strings.TrimSuffix(path.Base(source), extension)
	for index := 1; ; index++ {
		candidate := path.Join(directory, fmt.Sprintf("%s%s%d%s", stem, marker, index, extension))
		if !pkg.HasPart(candidate) {
			return candidate
		}
	}
}
