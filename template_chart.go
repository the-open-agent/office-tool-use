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

const (
	chartAxisDefaultFontPT       = 12.0
	chartAxisMinimumFontPT       = 8.0
	chartAxisMaximumLabelArea    = 0.40
	chartAxisMinimumPlotArea     = 0.55
	chartAxisDefaultOuterMargin  = 0.05
	chartAxisLabelSafetyFactor   = 0.90
	chartAxisCharacterWidthRatio = 0.52
)

type chartShape struct {
	ShapeID   string
	ShapeName string
	RelID     string
	WidthPX   float64
}

type chartCategoryAxis struct {
	Side       string
	FontSizePT float64
	PlotX      float64
	PlotWidth  float64
	HasLayout  bool
}

type chartCategoryLayout struct {
	Applicable              bool
	Changed                 bool
	Fits                    bool
	Side                    string
	FontSizePT              float64
	LabelArea               float64
	PlotX                   float64
	PlotWidth               float64
	LongestCategory         string
	LongestVisualWidth      float64
	SuggestedMaxVisualWidth float64
}

func applyChartEdits(pkg *ooxml.Package, slide *ooxml.Node, rels *ooxml.Relationships, types *ooxml.ContentTypes, sourceSlide int, newSlidePart string, edits []model.ChartEdit, nextChart, nextEmbedding *int) error {
	maps := map[string]chartShape{}
	for order, container := range ooxml.ChartContainers(slide) {
		id, name := ooxml.ShapeIdentity(container, order+1)
		chart := container.FirstDescendant(ooxml.NSChart, "chart")
		relID := ""
		if chart != nil {
			relID = chart.AttrValue(ooxml.NSOfficeRels, "id")
		}
		width := 0.0
		if geometry := ooxml.ContainerGeometry(container); geometry.Width != nil {
			width = float64(*geometry.Width)
		}
		info := chartShape{ShapeID: id, ShapeName: name, RelID: relID, WidthPX: width}
		maps[fmt.Sprintf("chart_id:s%02d_ch%s", sourceSlide, id)] = info
		maps["shape_id:"+id] = info
		if name != "" {
			maps["shape_name:"+name] = info
		}
	}
	cloned := map[string]string{}
	var missing []string
	for _, edit := range edits {
		selectors := chartSelectors(edit)
		var info chartShape
		found := false
		for _, selector := range selectors {
			if candidate, ok := maps[selector]; ok {
				info, found = candidate, true
				break
			}
		}
		if !found {
			if !edit.Optional {
				missing = append(missing, selectorLabel(selectors))
			}
			continue
		}
		rel, ok := rels.Find(info.RelID)
		if !ok || rel.Type != ooxml.RelationshipTypeChart {
			missing = append(missing, fmt.Sprintf("%s relationship=%s", selectorLabel(selectors), info.RelID))
			continue
		}
		newChartPart := cloned[info.RelID]
		if newChartPart == "" {
			sourceChart, err := ooxml.ResolveTarget(newSlidePart, rel.Target)
			if err != nil {
				return err
			}
			newChartPart = fmt.Sprintf("ppt/charts/chart%d.xml", *nextChart)
			*nextChart++
			if err := cloneAndUpdateChart(pkg, types, sourceChart, newChartPart, edit, info.WidthPX, nextEmbedding); err != nil {
				return err
			}
			target, err := ooxml.RelativeTarget(newSlidePart, newChartPart)
			if err != nil {
				return err
			}
			rel.Target = target
			cloned[info.RelID] = newChartPart
		} else {
			root, err := pkg.XMLPart(newChartPart)
			if err != nil {
				return err
			}
			if _, err := updateChartXML(root, edit, info.WidthPX); err != nil {
				return err
			}
			data, err := ooxml.MarshalXML(root)
			if err != nil {
				return err
			}
			if err := pkg.SetPart(newChartPart, data); err != nil {
				return err
			}
		}
	}
	if len(missing) != 0 {
		return fmt.Errorf("missing chart edit target(s) on slide %d: %s", sourceSlide, strings.Join(missing, "; "))
	}
	return nil
}

func cloneAndUpdateChart(pkg *ooxml.Package, types *ooxml.ContentTypes, source, target string, edit model.ChartEdit, chartWidthPX float64, nextEmbedding *int) error {
	root, err := pkg.XMLPart(source)
	if err != nil {
		return err
	}
	if _, err := updateChartXML(root, edit, chartWidthPX); err != nil {
		return err
	}
	data, err := ooxml.MarshalXML(root)
	if err != nil {
		return err
	}
	if err := pkg.SetPart(target, data); err != nil {
		return err
	}
	if err := types.EnsureOverride(target, ooxml.ContentTypeChart); err != nil {
		return err
	}
	sourceRels, err := pkg.Relationships(source)
	if err != nil {
		return err
	}
	if len(sourceRels.Items) == 0 {
		return nil
	}
	targetRels := &ooxml.Relationships{Items: append([]ooxml.Relationship(nil), sourceRels.Items...)}
	for index := range targetRels.Items {
		rel := &targetRels.Items[index]
		if rel.Mode != ooxml.TargetInternal || (rel.Type != ooxml.RelationshipTypeEmbeddedPackage && !strings.HasSuffix(strings.ToLower(rel.Target), ".xlsx")) {
			continue
		}
		workbook, err := ooxml.ResolveTarget(source, rel.Target)
		if err != nil || !pkg.HasPart(workbook) {
			continue
		}
		workbookData, err := pkg.ReadPart(workbook)
		if err != nil {
			return err
		}
		rewritten, err := rewriteChartWorkbook(workbookData, edit)
		if err != nil {
			return err
		}
		newWorkbook := fmt.Sprintf("ppt/embeddings/templateFillChart%d.xlsx", *nextEmbedding)
		*nextEmbedding++
		if err := pkg.SetPart(newWorkbook, rewritten); err != nil {
			return err
		}
		if err := types.EnsureOverride(newWorkbook, ooxml.ContentTypeEmbeddedXLSX); err != nil {
			return err
		}
		rel.Target, err = ooxml.RelativeTarget(target, newWorkbook)
		if err != nil {
			return err
		}
	}
	return pkg.SetRelationships(target, targetRels)
}

func updateChartXML(root *ooxml.Node, edit model.ChartEdit, chartWidthPX float64) (chartCategoryLayout, error) {
	if len(edit.Categories) == 0 || len(edit.Series) == 0 {
		return chartCategoryLayout{}, fmt.Errorf("chart edit requires non-empty categories and series")
	}
	plot := root.FirstDescendant(ooxml.NSChart, "plotArea")
	if plot == nil {
		return chartCategoryLayout{}, fmt.Errorf("chart XML has no plotArea")
	}
	var chartType *ooxml.Node
	for _, child := range plot.Children {
		if strings.HasSuffix(child.Name.Local, "Chart") && len(child.NamedChildren(ooxml.NSChart, "ser")) != 0 {
			chartType = child
			break
		}
	}
	if chartType == nil {
		return chartCategoryLayout{}, fmt.Errorf("chart XML has no editable series")
	}
	seriesNodes := chartType.NamedChildren(ooxml.NSChart, "ser")
	template := seriesNodes[len(seriesNodes)-1]
	for len(seriesNodes) < len(edit.Series) {
		clone := template.Clone()
		chartType.Children = append(chartType.Children, clone)
		seriesNodes = append(seriesNodes, clone)
	}
	keep := map[*ooxml.Node]bool{}
	for index := 0; index < len(edit.Series); index++ {
		keep[seriesNodes[index]] = true
	}
	filtered := chartType.Children[:0]
	for _, child := range chartType.Children {
		if child.Name.Space == ooxml.NSChart && child.Name.Local == "ser" && !keep[child] {
			continue
		}
		filtered = append(filtered, child)
	}
	chartType.Children = filtered
	seriesNodes = chartType.NamedChildren(ooxml.NSChart, "ser")
	for index, payload := range edit.Series {
		if len(payload.Values) != len(edit.Categories) {
			return chartCategoryLayout{}, fmt.Errorf("chart series values must match categories length")
		}
		series := seriesNodes[index]
		ensureChartChild(series, "idx").SetAttr("", "val", strconv.Itoa(index))
		ensureChartChild(series, "order").SetAttr("", "val", strconv.Itoa(index))
		setSeriesName(series, payload.Name, index+2)
		setCategoryCache(series, edit.Categories)
		setValueCache(series, payload.Values, index+2)
	}
	layout := adaptHorizontalCategoryAxis(root, edit.Categories, chartWidthPX)
	return layout, nil
}

func inspectHorizontalCategoryAxis(root *ooxml.Node) *chartCategoryAxis {
	if root == nil {
		return nil
	}
	plot := root.FirstDescendant(ooxml.NSChart, "plotArea")
	if plot == nil {
		return nil
	}
	var barChart *ooxml.Node
	for _, child := range plot.Children {
		if child.Name.Space != ooxml.NSChart || child.Name.Local != "barChart" {
			continue
		}
		if direction := child.Child(ooxml.NSChart, "barDir"); direction != nil && direction.AttrValue("", "val") == "bar" {
			barChart = child
			break
		}
	}
	if barChart == nil {
		return nil
	}
	axis := plot.Child(ooxml.NSChart, "catAx")
	if axis == nil {
		return nil
	}
	side := "l"
	if position := axis.Child(ooxml.NSChart, "axPos"); position != nil {
		side = position.AttrValue("", "val")
	}
	if side != "l" && side != "r" {
		return nil
	}
	fontSize := chartAxisDefaultFontPT
	if properties := axis.FirstDescendant(ooxml.NSDrawingML, "defRPr"); properties != nil {
		if raw, err := strconv.ParseFloat(properties.AttrValue("", "sz"), 64); err == nil && raw > 0 {
			fontSize = raw / 100
		}
	}
	result := &chartCategoryAxis{
		Side: side, FontSizePT: fontSize,
		PlotX: chartAxisDefaultOuterMargin, PlotWidth: 1 - 2*chartAxisDefaultOuterMargin,
	}
	if manual := chartManualLayout(plot); manual != nil {
		x, xOK := chartLayoutValue(manual, "x")
		width, widthOK := chartLayoutValue(manual, "w")
		if xOK && widthOK && x >= 0 && width > 0 && x+width <= 1.001 {
			result.PlotX = x
			result.PlotWidth = width
			result.HasLayout = true
		}
	}
	return result
}

func calculateHorizontalCategoryLayout(axis *chartCategoryAxis, categories []string, chartWidthPX float64) chartCategoryLayout {
	if axis == nil || chartWidthPX <= 0 || len(categories) == 0 {
		return chartCategoryLayout{}
	}
	longest, longestWidth := longestChartCategory(categories)
	result := chartCategoryLayout{
		Applicable: true, Fits: true, Side: axis.Side, FontSizePT: axis.FontSizePT,
		PlotX: axis.PlotX, PlotWidth: axis.PlotWidth,
		LongestCategory: longest, LongestVisualWidth: longestWidth,
	}
	leftMargin := clampFloat(axis.PlotX, 0, 1)
	rightMargin := clampFloat(1-axis.PlotX-axis.PlotWidth, 0, 1)
	currentArea := leftMargin
	oppositeMargin := rightMargin
	if axis.Side == "r" {
		currentArea = rightMargin
		oppositeMargin = leftMargin
	}
	maximumArea := math.Min(chartAxisMaximumLabelArea, 1-oppositeMargin-chartAxisMinimumPlotArea)
	maximumArea = math.Max(maximumArea, currentArea)
	required := chartCategoryRequiredFraction(longestWidth, axis.FontSizePT, chartWidthPX)
	if required <= currentArea {
		result.LabelArea = currentArea
		result.SuggestedMaxVisualWidth = chartCategoryCapacity(currentArea, axis.FontSizePT, chartWidthPX)
		return result
	}

	fontSize := axis.FontSizePT
	if required > maximumArea {
		fontSize = math.Max(chartAxisMinimumFontPT, axis.FontSizePT*maximumArea/required)
		fontSize = math.Min(fontSize, axis.FontSizePT)
		required = chartCategoryRequiredFraction(longestWidth, fontSize, chartWidthPX)
	}
	labelArea := math.Min(math.Max(required, currentArea), maximumArea)
	result.FontSizePT = fontSize
	result.LabelArea = labelArea
	result.Fits = required <= maximumArea+.000001
	result.SuggestedMaxVisualWidth = chartCategoryCapacity(maximumArea, fontSize, chartWidthPX)
	if axis.Side == "l" {
		rightEdge := axis.PlotX + axis.PlotWidth
		result.PlotX = labelArea
		result.PlotWidth = math.Max(rightEdge-labelArea, chartAxisMinimumPlotArea)
	} else {
		result.PlotX = axis.PlotX
		result.PlotWidth = math.Max(1-axis.PlotX-labelArea, chartAxisMinimumPlotArea)
	}
	result.Changed = math.Abs(result.PlotX-axis.PlotX) > .000001 ||
		math.Abs(result.PlotWidth-axis.PlotWidth) > .000001 ||
		math.Abs(result.FontSizePT-axis.FontSizePT) > .000001
	return result
}

func adaptHorizontalCategoryAxis(root *ooxml.Node, categories []string, chartWidthPX float64) chartCategoryLayout {
	axisInfo := inspectHorizontalCategoryAxis(root)
	layout := calculateHorizontalCategoryLayout(axisInfo, categories, chartWidthPX)
	if !layout.Applicable || !layout.Changed {
		return layout
	}
	plot := root.FirstDescendant(ooxml.NSChart, "plotArea")
	axis := plot.Child(ooxml.NSChart, "catAx")
	manual := ensureChartManualLayout(plot, axisInfo)
	setChartLayoutValue(manual, "x", layout.PlotX)
	setChartLayoutValue(manual, "w", layout.PlotWidth)
	setChartAxisFontSize(axis, layout.FontSizePT)
	return layout
}

func longestChartCategory(categories []string) (string, float64) {
	longest := ""
	width := 0.0
	for _, category := range categories {
		candidate := ooxml.VisualWidth(category)
		if candidate > width {
			longest = category
			width = candidate
		}
	}
	return longest, width
}

func chartCategoryRequiredFraction(visualUnits, fontSizePT, chartWidthPX float64) float64 {
	fontPX := fontSizePT * ooxml.PXPerInch / 72
	padding := fontPX * .5
	requiredPX := (visualUnits*fontPX*chartAxisCharacterWidthRatio + padding) / chartAxisLabelSafetyFactor
	return requiredPX / chartWidthPX
}

func chartCategoryCapacity(labelArea, fontSizePT, chartWidthPX float64) float64 {
	fontPX := fontSizePT * ooxml.PXPerInch / 72
	available := labelArea*chartWidthPX*chartAxisLabelSafetyFactor - fontPX*.5
	return math.Max(available/(fontPX*chartAxisCharacterWidthRatio), 0)
}

func chartManualLayout(plot *ooxml.Node) *ooxml.Node {
	if layout := plot.Child(ooxml.NSChart, "layout"); layout != nil {
		return layout.Child(ooxml.NSChart, "manualLayout")
	}
	return nil
}

func ensureChartManualLayout(plot *ooxml.Node, axis *chartCategoryAxis) *ooxml.Node {
	layout := plot.Child(ooxml.NSChart, "layout")
	if layout == nil {
		layout = ooxml.Element(ooxml.NSChart, "layout")
		plot.Children = append([]*ooxml.Node{layout}, plot.Children...)
	}
	manual := layout.Child(ooxml.NSChart, "manualLayout")
	if manual == nil {
		manual = ooxml.Element(ooxml.NSChart, "manualLayout")
		manual.Children = []*ooxml.Node{
			ooxml.Element(ooxml.NSChart, "layoutTarget", ooxml.PlainAttr("val", "inner")),
			ooxml.Element(ooxml.NSChart, "xMode", ooxml.PlainAttr("val", "edge")),
			ooxml.Element(ooxml.NSChart, "x", ooxml.PlainAttr("val", formatChartFraction(axis.PlotX))),
			ooxml.Element(ooxml.NSChart, "w", ooxml.PlainAttr("val", formatChartFraction(axis.PlotWidth))),
		}
		layout.Children = append(layout.Children, manual)
	}
	return manual
}

func chartLayoutValue(manual *ooxml.Node, local string) (float64, bool) {
	node := manual.Child(ooxml.NSChart, local)
	if node == nil {
		return 0, false
	}
	value, err := strconv.ParseFloat(node.AttrValue("", "val"), 64)
	return value, err == nil
}

func setChartLayoutValue(manual *ooxml.Node, local string, value float64) {
	node := manual.Child(ooxml.NSChart, local)
	if node == nil {
		node = ooxml.Element(ooxml.NSChart, local)
		manual.Children = append(manual.Children, node)
	}
	node.SetAttr("", "val", formatChartFraction(value))
}

func formatChartFraction(value float64) string {
	return strconv.FormatFloat(clampFloat(value, 0, 1), 'f', 8, 64)
}

func setChartAxisFontSize(axis *ooxml.Node, fontSizePT float64) {
	if axis == nil {
		return
	}
	txPr := axis.Child(ooxml.NSChart, "txPr")
	if txPr == nil {
		txPr = ooxml.Element(ooxml.NSChart, "txPr")
		insertBeforeChartChild(axis, txPr, "crossAx")
	}
	bodyPr := txPr.Child(ooxml.NSDrawingML, "bodyPr")
	if bodyPr == nil {
		bodyPr = ooxml.Element(ooxml.NSDrawingML, "bodyPr")
		txPr.Children = append([]*ooxml.Node{bodyPr}, txPr.Children...)
	}
	paragraph := txPr.Child(ooxml.NSDrawingML, "p")
	if paragraph == nil {
		paragraph = ooxml.Element(ooxml.NSDrawingML, "p")
		txPr.Children = append(txPr.Children, paragraph)
	}
	paragraphProperties := paragraph.Child(ooxml.NSDrawingML, "pPr")
	if paragraphProperties == nil {
		paragraphProperties = ooxml.Element(ooxml.NSDrawingML, "pPr")
		paragraph.Children = append([]*ooxml.Node{paragraphProperties}, paragraph.Children...)
	}
	defaultRun := paragraphProperties.Child(ooxml.NSDrawingML, "defRPr")
	if defaultRun == nil {
		defaultRun = ooxml.Element(ooxml.NSDrawingML, "defRPr")
		paragraphProperties.Children = append(paragraphProperties.Children, defaultRun)
	}
	endRun := paragraph.Child(ooxml.NSDrawingML, "endParaRPr")
	if endRun == nil {
		endRun = ooxml.Element(ooxml.NSDrawingML, "endParaRPr")
		paragraph.Children = append(paragraph.Children, endRun)
	}
	raw := strconv.Itoa(int(math.Round(fontSizePT * 100)))
	for _, local := range []string{"rPr", "defRPr", "endParaRPr"} {
		for _, properties := range txPr.Descendants(ooxml.NSDrawingML, local) {
			properties.SetAttr("", "sz", raw)
		}
	}
}

func insertBeforeChartChild(parent, child *ooxml.Node, before string) {
	for index, candidate := range parent.Children {
		if candidate.Name.Space == ooxml.NSChart && candidate.Name.Local == before {
			parent.Children = append(parent.Children, nil)
			copy(parent.Children[index+1:], parent.Children[index:])
			parent.Children[index] = child
			return
		}
	}
	parent.Children = append(parent.Children, child)
}

func clampFloat(value, low, high float64) float64 {
	return math.Min(math.Max(value, low), high)
}

func ensureChartChild(parent *ooxml.Node, local string) *ooxml.Node {
	if child := parent.Child(ooxml.NSChart, local); child != nil {
		return child
	}
	child := ooxml.Element(ooxml.NSChart, local)
	parent.Children = append(parent.Children, child)
	return child
}

func setSeriesName(series *ooxml.Node, name string, column int) {
	tx := ensureChartChild(series, "tx")
	tx.Children = nil
	ref := ooxml.Element(ooxml.NSChart, "strRef")
	formula := ooxml.Element(ooxml.NSChart, "f")
	formula.Text = fmt.Sprintf("Sheet1!$%s$1", excelColumn(column))
	cache := ooxml.Element(ooxml.NSChart, "strCache")
	writeChartCache(cache, []interface{}{name}, false)
	ref.Children = []*ooxml.Node{formula, cache}
	tx.Children = []*ooxml.Node{ref}
}

func setCategoryCache(series *ooxml.Node, categories []string) {
	category := ensureChartChild(series, "cat")
	ref := ooxml.Element(ooxml.NSChart, "strRef")
	formula := ooxml.Element(ooxml.NSChart, "f")
	formula.Text = fmt.Sprintf("Sheet1!$A$2:$A$%d", len(categories)+1)
	cache := ooxml.Element(ooxml.NSChart, "strCache")
	values := make([]interface{}, len(categories))
	for index := range categories {
		values[index] = categories[index]
	}
	writeChartCache(cache, values, false)
	ref.Children = []*ooxml.Node{formula, cache}
	category.Children = []*ooxml.Node{ref}
}

func setValueCache(series *ooxml.Node, values []interface{}, column int) {
	value := ensureChartChild(series, "val")
	ref := ooxml.Element(ooxml.NSChart, "numRef")
	formula := ooxml.Element(ooxml.NSChart, "f")
	formula.Text = fmt.Sprintf("Sheet1!$%s$2:$%s$%d", excelColumn(column), excelColumn(column), len(values)+1)
	cache := ooxml.Element(ooxml.NSChart, "numCache")
	writeChartCache(cache, values, true)
	ref.Children = []*ooxml.Node{formula, cache}
	value.Children = []*ooxml.Node{ref}
}

func writeChartCache(cache *ooxml.Node, values []interface{}, numeric bool) {
	cache.Children = nil
	if numeric {
		format := ooxml.Element(ooxml.NSChart, "formatCode")
		format.Text = "General"
		cache.Children = append(cache.Children, format)
	}
	count := ooxml.Element(ooxml.NSChart, "ptCount")
	count.SetAttr("", "val", strconv.Itoa(len(values)))
	cache.Children = append(cache.Children, count)
	for index, value := range values {
		point := ooxml.Element(ooxml.NSChart, "pt", ooxml.PlainAttr("idx", strconv.Itoa(index)))
		node := ooxml.Element(ooxml.NSChart, "v")
		node.Text = fmt.Sprint(value)
		point.Children = []*ooxml.Node{node}
		cache.Children = append(cache.Children, point)
	}
}

func excelColumn(index int) string {
	var result string
	for index > 0 {
		index--
		result = string(rune('A'+index%26)) + result
		index /= 26
	}
	if result == "" {
		return "A"
	}
	return result
}

func rewriteChartWorkbook(data []byte, edit model.ChartEdit) ([]byte, error) {
	workbook, err := ooxml.OpenBytes(data, ooxml.DefaultLimits())
	if err != nil {
		return nil, err
	}
	sheetPart := "xl/worksheets/sheet1.xml"
	if root, err := workbook.XMLPart("xl/workbook.xml"); err == nil {
		if sheets := root.FirstDescendant(ooxml.NSSpreadsheetML, "sheets"); sheets != nil && len(sheets.Children) != 0 {
			relID := sheets.Children[0].AttrValue(ooxml.NSOfficeRels, "id")
			if rels, relErr := workbook.Relationships("xl/workbook.xml"); relErr == nil {
				if rel, ok := rels.Find(relID); ok {
					if resolved, resolveErr := ooxml.ResolveTarget("xl/workbook.xml", rel.Target); resolveErr == nil {
						sheetPart = resolved
					}
				}
			}
		}
	}
	if !workbook.HasPart(sheetPart) {
		return data, nil
	}
	sheet, err := workbook.XMLPart(sheetPart)
	if err != nil {
		return nil, err
	}
	sheetData := sheet.FirstDescendant(ooxml.NSSpreadsheetML, "sheetData")
	if sheetData == nil {
		sheetData = ooxml.Element(ooxml.NSSpreadsheetML, "sheetData")
		sheet.Children = append(sheet.Children, sheetData)
	}
	sheetData.Children = nil
	rows := make([][]interface{}, 0, len(edit.Categories)+1)
	header := []interface{}{"Category"}
	for index, series := range edit.Series {
		name := series.Name
		if name == "" {
			name = fmt.Sprintf("Series %d", index+1)
		}
		header = append(header, name)
	}
	rows = append(rows, header)
	for rowIndex, category := range edit.Categories {
		row := []interface{}{category}
		for _, series := range edit.Series {
			row = append(row, series.Values[rowIndex])
		}
		rows = append(rows, row)
	}
	for rowIndex, values := range rows {
		row := ooxml.Element(ooxml.NSSpreadsheetML, "row", ooxml.PlainAttr("r", strconv.Itoa(rowIndex+1)))
		for colIndex, value := range values {
			row.Children = append(row.Children, spreadsheetCell(value, rowIndex+1, colIndex+1))
		}
		sheetData.Children = append(sheetData.Children, row)
	}
	xmlData, err := ooxml.MarshalXML(sheet)
	if err != nil {
		return nil, err
	}
	if err := workbook.SetPart(sheetPart, xmlData); err != nil {
		return nil, err
	}
	return workbook.Bytes()
}

func spreadsheetCell(value interface{}, row, col int) *ooxml.Node {
	cell := ooxml.Element(ooxml.NSSpreadsheetML, "c", ooxml.PlainAttr("r", fmt.Sprintf("%s%d", excelColumn(col), row)))
	switch value.(type) {
	case int, int32, int64, float32, float64:
		node := ooxml.Element(ooxml.NSSpreadsheetML, "v")
		node.Text = fmt.Sprint(value)
		cell.Children = []*ooxml.Node{node}
	default:
		cell.SetAttr("", "t", "inlineStr")
		inline := ooxml.Element(ooxml.NSSpreadsheetML, "is")
		text := ooxml.Element(ooxml.NSSpreadsheetML, "t")
		text.Text = fmt.Sprint(value)
		inline.Children = []*ooxml.Node{text}
		cell.Children = []*ooxml.Node{inline}
	}
	return cell
}
