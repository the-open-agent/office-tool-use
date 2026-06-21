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
	"path/filepath"
	"strconv"
	"strings"

	"github.com/the-open-agent/office-tool-use/model"
	"github.com/the-open-agent/office-tool-use/ooxml"
	"github.com/the-open-agent/office-tool-use/smartart"
)

var (
	thanksKeywords  = []string{"thank", "thanks", "q&a", "qa", "contact", "致谢", "谢谢", "感谢", "答疑", "联系方式"}
	tocKeywords     = []string{"agenda", "contents", "content", "outline", "目录", "议程"}
	chapterKeywords = []string{"chapter", "part", "section", "章节", "部分"}
)

func AnalyzeFile(pptxPath string, limits ooxml.Limits) (*model.Library, error) {
	absolute, err := filepath.Abs(pptxPath)
	if err != nil {
		return nil, err
	}
	pkg, err := ooxml.OpenFile(absolute, limits)
	if err != nil {
		return nil, err
	}
	return Analyze(pkg, absolute)
}

func Analyze(pkg *ooxml.Package, sourcePPTX string) (*model.Library, error) {
	if pkg == nil {
		return nil, fmt.Errorf("package cannot be nil")
	}
	if err := pkg.ValidatePresentation(); err != nil {
		return nil, err
	}
	presentation, err := pkg.XMLPart("ppt/presentation.xml")
	if err != nil {
		return nil, err
	}
	refs, err := pkg.SlideRefs()
	if err != nil {
		return nil, err
	}
	library := &model.Library{
		Schema:     model.LibrarySchema,
		SourcePPTX: sourcePPTX,
		CanvasPX:   analyzeCanvas(presentation),
		Slides:     make([]model.SlideLibraryItem, 0, len(refs)),
	}
	for _, ref := range refs {
		slide, err := pkg.XMLPart(ref.PartName)
		if err != nil {
			return nil, err
		}
		layout, master, err := pkg.InheritanceRoots(ref.PartName)
		if err != nil {
			return nil, err
		}
		objects := analyzeObjects(slide)
		objectByID := make(map[string]*model.SlideObject, len(objects))
		for index := range objects {
			objectByID[objects[index].ShapeID] = &objects[index]
		}
		slots := analyzeSlots(slide, layout, master, ref.Index, objectByID)
		tables := analyzeTables(slide, ref.Index)
		charts, err := analyzeCharts(pkg, slide, ref)
		if err != nil {
			return nil, err
		}
		images, err := analyzeImages(pkg, slide, ref, objectByID)
		if err != nil {
			return nil, err
		}
		smartArts, err := smartart.AnalyzeSmartArts(pkg, slide, ref, objectByID)
		if err != nil {
			return nil, err
		}
		var textParts []string
		for _, slot := range slots {
			if slot.Text != "" {
				textParts = append(textParts, slot.Text)
			}
		}
		slideText := strings.Join(textParts, "\n")
		library.Slides = append(library.Slides, model.SlideLibraryItem{
			SlideIndex: ref.Index, PageType: classifyPageType(ref.Index, len(refs), slideText, len(slots)),
			TextSummary: ooxml.TruncateRunes(slideText, 500), Slots: slots, Tables: tables, Charts: charts,
			Images: images, SmartArts: smartArts, Objects: objects,
		})
	}
	library.SlideCount = len(library.Slides)
	library.PlanContract = planContractForLibrary(library)
	return library, nil
}

func planContractForLibrary(library *model.Library) interface{} {
	slides := make([]interface{}, 0, len(library.Slides))
	exampleEdits := planContractExampleEdits()
	for index, slide := range library.Slides {
		item := map[string]interface{}{
			"source_slide": slide.SlideIndex,
			"purpose":      slide.PageType,
		}
		if index == 0 {
			for key, value := range exampleEdits {
				item[key] = value
			}
		}
		slides = append(slides, item)
	}
	return map[string]interface{}{
		"schema":        model.PlanSchema,
		"source_pptx":   library.SourcePPTX,
		"usage":         "plan.slides is the generated output slide list, not a patch list. To preserve every source slide, keep one entry for each item in library.slides and add edits only to the slides you want to change.",
		"slides":        slides,
		"example_edits": exampleEdits,
	}
}

func planContractExampleEdits() map[string]interface{} {
	return map[string]interface{}{
		"replacements": []interface{}{map[string]interface{}{
			"slot_id": "s01_sh2", "text": "Replacement text", "preserve_line_breaks": false,
		}},
		"table_edits": []interface{}{map[string]interface{}{
			"table_id": "s01_tbl3",
			"cells":    []interface{}{map[string]interface{}{"row": 0, "col": 0, "text": "Replacement cell text"}},
		}},
		"chart_edits": []interface{}{map[string]interface{}{
			"chart_id": "s01_ch4", "categories": []string{"A", "B"},
			"series": []interface{}{map[string]interface{}{"name": "Series 1", "values": []int{1, 2}}},
		}},
		"image_edits": []interface{}{map[string]interface{}{
			"image_id": "s01_img5", "image_path": "https://example.com/image.png",
		}},
		"smartart_edits": []interface{}{map[string]interface{}{
			"smartart_id": "s01_sa4",
			"resize":      false,
			"nodes": []interface{}{
				map[string]interface{}{"node_id": "s01_sa4_n01", "text": "Replaced node text"},
				map[string]interface{}{"node_id": "s01_sa4_n02", "paragraphs": []string{"Line 1", "Line 2"}},
			},
		}},
	}
}

func analyzeCanvas(root *ooxml.Node) model.Canvas {
	size := root.FirstDescendant(ooxml.NSPresentation, "sldSz")
	if size == nil {
		return model.Canvas{}
	}
	return model.Canvas{Width: ooxml.EMUToPX(size.AttrValue("", "cx")), Height: ooxml.EMUToPX(size.AttrValue("", "cy"))}
}

func analyzeSlots(slide, layout, master *ooxml.Node, slideIndex int, objectByID map[string]*model.SlideObject) []model.TextSlot {
	containers := ooxml.TextContainers(slide)
	result := make([]model.TextSlot, 0, len(containers))
	for order, container := range containers {
		shapeID, shapeName := ooxml.ShapeIdentity(container, order+1)
		paragraphs := ooxml.ParagraphTexts(container)
		text := strings.Join(paragraphs, "\n")
		chain := ooxml.InheritedChain(container, layout, master)
		geometry := ooxml.ContainerGeometry(container)
		for _, inherited := range chain {
			candidate := ooxml.ContainerGeometry(inherited)
			if candidate.Width != nil && candidate.Height != nil {
				geometry = candidate
				break
			}
		}
		metrics := ooxml.TextMetricsForChain(chain, len(paragraphs))
		placeholder, _, _ := ooxml.PlaceholderKey(container)
		role := ooxml.SlotRole(text, shapeName, placeholder, metrics, geometry, len(paragraphs), len(container.Descendants(ooxml.NSDrawingML, "t")))
		var zOrder *int
		if object := objectByID[shapeID]; object != nil {
			object.Geometry = geometry
			object.HasText = text != ""
			zOrder = ooxml.PtrInt(object.ZOrder)
		}
		result = append(result, model.TextSlot{
			SlotID: fmt.Sprintf("s%02d_sh%s", slideIndex, shapeID), ShapeID: shapeID, ShapeName: shapeName,
			Role: role, Text: text, ParagraphCount: len(paragraphs), Geometry: geometry, TextMetrics: metrics,
			SingleLine: (role == "title_candidate" || role == "label_candidate") && !strings.Contains(text, "\n") && metrics.Wrap == "none",
			ZOrder:     zOrder,
		})
	}
	return result
}

func analyzeTables(slide *ooxml.Node, slideIndex int) []model.TableInfo {
	containers := ooxml.TableContainers(slide)
	result := make([]model.TableInfo, 0, len(containers))
	for order, container := range containers {
		shapeID, shapeName := ooxml.ShapeIdentity(container, order+1)
		geometry := ooxml.ContainerGeometry(container)
		table := container.FirstDescendant(ooxml.NSDrawingML, "tbl")
		rows := table.NamedChildren(ooxml.NSDrawingML, "tr")
		totalHeight := 0
		for _, row := range rows {
			if value, ok := ooxml.IntAttr(row, "", "h"); ok {
				totalHeight += value
			}
		}
		y := valueOrZero(geometry.Y)
		info := model.TableInfo{
			TableID: fmt.Sprintf("s%02d_tbl%s", slideIndex, shapeID), ShapeID: shapeID, ShapeName: shapeName,
			Geometry: geometry, RowCount: len(rows), Rows: make([]model.TableRow, 0, len(rows)),
		}
		for rowIndex, row := range rows {
			cells := row.NamedChildren(ooxml.NSDrawingML, "tc")
			info.ColumnCount = max(info.ColumnCount, len(cells))
			rowHeight := 0
			if geometry.Height != nil {
				if raw, ok := ooxml.IntAttr(row, "", "h"); ok && totalHeight > 0 {
					rowHeight = int(math.Round(float64(raw) / float64(totalHeight) * float64(*geometry.Height)))
				} else {
					rowHeight = int(math.Round(float64(*geometry.Height) / float64(max(len(rows), 1))))
				}
			}
			cellWidth := 0
			if geometry.Width != nil {
				cellWidth = int(math.Round(float64(*geometry.Width) / float64(max(len(cells), 1))))
			}
			rowInfo := model.TableRow{Row: rowIndex, Cells: make([]model.TableCell, 0, len(cells))}
			for colIndex, cell := range cells {
				paragraphs := ooxml.ParagraphTexts(cell)
				x := valueOrZero(geometry.X) + colIndex*cellWidth
				cellGeometry := model.Geometry{X: ooxml.PtrInt(x), Y: ooxml.PtrInt(y), Width: ooxml.PtrInt(cellWidth), Height: ooxml.PtrInt(rowHeight)}
				rowInfo.Cells = append(rowInfo.Cells, model.TableCell{
					Row: rowIndex, Col: colIndex, Text: strings.Join(paragraphs, "\n"), ParagraphCount: len(paragraphs),
					Geometry: cellGeometry, TextMetrics: ooxml.TextMetricsForChain([]*ooxml.Node{cell}, len(paragraphs)),
				})
			}
			info.Rows = append(info.Rows, rowInfo)
			y += rowHeight
		}
		result = append(result, info)
	}
	return result
}

func analyzeCharts(pkg *ooxml.Package, slide *ooxml.Node, ref ooxml.SlideRef) ([]model.ChartInfo, error) {
	rels, err := pkg.Relationships(ref.PartName)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]ooxml.Relationship, len(rels.Items))
	for _, rel := range rels.Items {
		byID[rel.ID] = rel
	}
	containers := ooxml.ChartContainers(slide)
	result := make([]model.ChartInfo, 0, len(containers))
	for order, container := range containers {
		shapeID, shapeName := ooxml.ShapeIdentity(container, order+1)
		info := model.ChartInfo{
			ChartID: fmt.Sprintf("s%02d_ch%s", ref.Index, shapeID), ShapeID: shapeID, ShapeName: shapeName,
			Categories: []interface{}{}, Series: []model.ChartSeries{},
			Geometry: ooxml.ContainerGeometry(container),
		}
		chartRef := container.FirstDescendant(ooxml.NSChart, "chart")
		if chartRef != nil {
			rel := byID[chartRef.AttrValue(ooxml.NSOfficeRels, "id")]
			if rel.Type == ooxml.RelationshipTypeChart && rel.Mode == ooxml.TargetInternal {
				partName, resolveErr := ooxml.ResolveTarget(ref.PartName, rel.Target)
				if resolveErr == nil && pkg.HasPart(partName) {
					root, parseErr := pkg.XMLPart(partName)
					if parseErr == nil {
						readChartData(root, &info)
						info.CategoryAxis = inspectHorizontalCategoryAxis(root)
					}
				}
			}
		}
		result = append(result, info)
	}
	return result, nil
}

func readChartData(root *ooxml.Node, info *model.ChartInfo) {
	plot := root.FirstDescendant(ooxml.NSChart, "plotArea")
	if plot == nil {
		return
	}
	var chartType *ooxml.Node
	for _, child := range plot.Children {
		if strings.HasSuffix(child.Name.Local, "Chart") && len(child.NamedChildren(ooxml.NSChart, "ser")) != 0 {
			chartType = child
			break
		}
	}
	if chartType == nil {
		return
	}
	kind := chartType.Name.Local
	info.ChartType = &kind
	seriesNodes := chartType.NamedChildren(ooxml.NSChart, "ser")
	if len(seriesNodes) != 0 {
		info.Categories = chartCacheValues(seriesNodes[0].Child(ooxml.NSChart, "cat"), false)
	}
	for index, series := range seriesNodes {
		name := fmt.Sprintf("Series %d", index+1)
		if tx := series.Child(ooxml.NSChart, "tx"); tx != nil {
			values := chartCacheValues(tx, false)
			if len(values) != 0 {
				name = fmt.Sprint(values[0])
			} else if direct := tx.Child(ooxml.NSChart, "v"); direct != nil && strings.TrimSpace(ooxml.TextContent(direct)) != "" {
				name = strings.TrimSpace(ooxml.TextContent(direct))
			}
		}
		info.Series = append(info.Series, model.ChartSeries{Name: name, Values: chartCacheValues(series.Child(ooxml.NSChart, "val"), true)})
	}
	info.CategoryCount = len(info.Categories)
	info.SeriesCount = len(info.Series)
}

func chartCacheValues(parent *ooxml.Node, numeric bool) []interface{} {
	if parent == nil {
		return []interface{}{}
	}
	cache := parent.FirstDescendant(ooxml.NSChart, "strCache")
	if cache == nil {
		cache = parent.FirstDescendant(ooxml.NSChart, "numCache")
	}
	if cache == nil {
		return []interface{}{}
	}
	result := make([]interface{}, 0)
	for _, point := range cache.NamedChildren(ooxml.NSChart, "pt") {
		valueNode := point.Child(ooxml.NSChart, "v")
		value := ""
		if valueNode != nil {
			value = ooxml.TextContent(valueNode)
		}
		if numeric {
			if number, err := strconv.ParseFloat(value, 64); err == nil {
				if number == math.Trunc(number) {
					result = append(result, int(number))
				} else {
					result = append(result, number)
				}
				continue
			}
		}
		result = append(result, value)
	}
	return result
}

func analyzeObjects(slide *ooxml.Node) []model.SlideObject {
	tree := slide.FirstDescendant(ooxml.NSPresentation, "spTree")
	if tree == nil {
		return []model.SlideObject{}
	}
	var result []model.SlideObject
	var walk func(*ooxml.Node, *objectTransform)
	walk = func(parent *ooxml.Node, parentTransform *objectTransform) {
		for _, child := range parent.Children {
			if child.Name.Space != ooxml.NSPresentation {
				continue
			}
			if child.Name.Local == "grpSp" {
				groupTransform := readObjectTransform(child, true)
				if groupTransform != nil && parentTransform != nil {
					absolute := absoluteObjectGeometry(groupTransform, parentTransform)
					groupTransform.X = float64(*absolute.X) / ooxml.PXPerInch * ooxml.EMUPerInch
					groupTransform.Y = float64(*absolute.Y) / ooxml.PXPerInch * ooxml.EMUPerInch
					groupTransform.Width = float64(*absolute.Width) / ooxml.PXPerInch * ooxml.EMUPerInch
					groupTransform.Height = float64(*absolute.Height) / ooxml.PXPerInch * ooxml.EMUPerInch
				}
				walk(child, groupTransform)
				continue
			}
			switch child.Name.Local {
			case "sp", "pic", "cxnSp", "graphicFrame":
			default:
				continue
			}
			shapeID, shapeName := ooxml.ShapeIdentity(child, len(result)+1)
			geometry := model.Geometry{}
			if transform := readObjectTransform(child, false); transform != nil {
				geometry = absoluteObjectGeometry(transform, parentTransform)
			} else {
				rotation := 0.0
				geometry.Rotation = &rotation
			}
			result = append(result, model.SlideObject{
				ShapeID: shapeID, ShapeName: shapeName, Kind: child.Name.Local,
				Geometry: geometry, ZOrder: len(result), HasText: len(ooxml.ParagraphTexts(child)) != 0,
			})
		}
	}
	walk(tree, nil)
	return result
}

type objectTransform struct {
	X, Y, Width, Height                     float64
	Rotation                                float64
	ChildX, ChildY, ChildWidth, ChildHeight float64
}

func readObjectTransform(node *ooxml.Node, group bool) *objectTransform {
	var transform *ooxml.Node
	if group {
		if properties := node.Child(ooxml.NSPresentation, "grpSpPr"); properties != nil {
			transform = properties.Child(ooxml.NSDrawingML, "xfrm")
		}
	} else if node.Name.Local == "graphicFrame" {
		transform = node.Child(ooxml.NSPresentation, "xfrm")
	} else if properties := node.Child(ooxml.NSPresentation, "spPr"); properties != nil {
		transform = properties.Child(ooxml.NSDrawingML, "xfrm")
	}
	if transform == nil {
		return nil
	}
	offset, extent := transform.Child(ooxml.NSDrawingML, "off"), transform.Child(ooxml.NSDrawingML, "ext")
	if offset == nil || extent == nil {
		return nil
	}
	x, xOK := ooxml.FloatAttr(offset, "", "x")
	y, yOK := ooxml.FloatAttr(offset, "", "y")
	width, widthOK := ooxml.FloatAttr(extent, "", "cx")
	height, heightOK := ooxml.FloatAttr(extent, "", "cy")
	if !xOK || !yOK || !widthOK || !heightOK {
		return nil
	}
	result := &objectTransform{X: x, Y: y, Width: width, Height: height}
	if rotation, ok := ooxml.FloatAttr(transform, "", "rot"); ok {
		result.Rotation = rotation / 60000
	}
	childOffset, childExtent := transform.Child(ooxml.NSDrawingML, "chOff"), transform.Child(ooxml.NSDrawingML, "chExt")
	if childOffset != nil && childExtent != nil {
		result.ChildX, _ = ooxml.FloatAttr(childOffset, "", "x")
		result.ChildY, _ = ooxml.FloatAttr(childOffset, "", "y")
		result.ChildWidth, _ = ooxml.FloatAttr(childExtent, "", "cx")
		result.ChildHeight, _ = ooxml.FloatAttr(childExtent, "", "cy")
	}
	return result
}

func absoluteObjectGeometry(value, parent *objectTransform) model.Geometry {
	x, y, width, height := value.X, value.Y, value.Width, value.Height
	if parent != nil && parent.ChildWidth != 0 && parent.ChildHeight != 0 {
		scaleX, scaleY := parent.Width/parent.ChildWidth, parent.Height/parent.ChildHeight
		x = parent.X + (value.X-parent.ChildX)*scaleX
		y = parent.Y + (value.Y-parent.ChildY)*scaleY
		width, height = value.Width*scaleX, value.Height*scaleY
	}
	rotation := math.Mod(value.Rotation, 360)
	if parent != nil {
		rotation = math.Mod(rotation+parent.Rotation, 360)
	}
	if rotation != 0 {
		radians := rotation * math.Pi / 180
		boundingWidth := math.Abs(width*math.Cos(radians)) + math.Abs(height*math.Sin(radians))
		boundingHeight := math.Abs(width*math.Sin(radians)) + math.Abs(height*math.Cos(radians))
		x += (width - boundingWidth) / 2
		y += (height - boundingHeight) / 2
		width, height = boundingWidth, boundingHeight
	}
	px := func(value float64) *int {
		result := int(math.Round(value / ooxml.EMUPerInch * ooxml.PXPerInch))
		return &result
	}
	rotation = math.Round(rotation*100) / 100
	return model.Geometry{X: px(x), Y: px(y), Width: px(width), Height: px(height), Rotation: &rotation}
}

func classifyPageType(index, total int, text string, slotCount int) string {
	normalized := strings.ToLower(text)
	if index == 1 {
		return "cover_candidate"
	}
	if index == total || containsAny(normalized, thanksKeywords) {
		return "ending_candidate"
	}
	if containsAny(normalized, tocKeywords) {
		return "toc_candidate"
	}
	if containsAny(normalized, chapterKeywords) || (slotCount <= 2 && len([]rune(text)) <= 80) {
		return "chapter_candidate"
	}
	return "content_candidate"
}

func containsAny(value string, terms []string) bool {
	for _, term := range terms {
		if strings.Contains(value, term) {
			return true
		}
	}
	return false
}

func valueOrZero(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}
