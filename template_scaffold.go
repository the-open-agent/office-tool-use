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

	"github.com/the-open-agent/office-tool-use/model"
)

func ScaffoldPlan(library *model.Library, selectedSlides []int, includeEmpty bool) *model.Plan {
	selected := make(map[int]struct{}, len(selectedSlides))
	for _, index := range selectedSlides {
		selected[index] = struct{}{}
	}
	source := library.Slides
	if len(selected) == 0 && len(source) > 6 {
		source = source[:6]
	}
	plan := &model.Plan{Schema: model.PlanSchema, SourcePPTX: library.SourcePPTX}
	for _, slide := range source {
		if len(selected) != 0 {
			if _, ok := selected[slide.SlideIndex]; !ok {
				continue
			}
		}
		item := model.PlanSlide{SourceSlide: slide.SlideIndex, Purpose: slide.PageType}
		for _, slot := range slide.Slots {
			if !includeEmpty && slot.Text == "" {
				continue
			}
			item.Replacements = append(item.Replacements, model.Replacement{SlotID: slot.SlotID, OldText: slot.Text, Text: slot.Text})
		}
		for _, table := range slide.Tables {
			edit := model.TableEdit{TableID: table.TableID}
			for _, row := range table.Rows {
				for _, cell := range row.Cells {
					edit.Cells = append(edit.Cells, model.TableCellEdit{Row: cell.Row, Col: cell.Col, OldText: cell.Text, Text: cell.Text})
				}
			}
			item.TableEdits = append(item.TableEdits, edit)
		}
		for _, chart := range slide.Charts {
			series := chart.Series
			if len(series) == 0 {
				series = []model.ChartSeries{{Name: "Series 1", Values: []interface{}{}}}
			}
			categories := make([]string, len(chart.Categories))
			for index, value := range chart.Categories {
				categories[index] = fmt.Sprint(value)
			}
			item.ChartEdits = append(item.ChartEdits, model.ChartEdit{ChartID: chart.ChartID, Categories: categories, Series: series})
		}
		plan.Slides = append(plan.Slides, item)
	}
	return plan
}
