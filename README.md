<div align="center">

# office-tool-use

**Standalone Go library for bounded OOXML/PPTX package handling and PowerPoint template operations**

*Extracted from [OpenAgent](https://github.com/the-open-agent/openagent)'s PowerPoint tool layer*

<br/>

[![Build](https://github.com/the-open-agent/office-tool-use/workflows/Build/badge.svg?style=flat-square)](https://github.com/the-open-agent/office-tool-use/actions/workflows/build.yml)
[![Release](https://img.shields.io/github/v/release/the-open-agent/office-tool-use?style=flat-square&color=4f46e5)](https://github.com/the-open-agent/office-tool-use/releases/latest)
[![Go Reference](https://pkg.go.dev/badge/github.com/the-open-agent/office-tool-use.svg)](https://pkg.go.dev/github.com/the-open-agent/office-tool-use)
[![Go Report](https://goreportcard.com/badge/github.com/the-open-agent/office-tool-use?style=flat-square)](https://goreportcard.com/report/github.com/the-open-agent/office-tool-use)
[![License](https://img.shields.io/github/license/the-open-agent/office-tool-use?style=flat-square&color=22c55e)](https://github.com/the-open-agent/office-tool-use/blob/master/LICENSE)

</div>

---

## What is this?

`office-tool-use` is the native Go implementation behind OpenAgent's PowerPoint template tools, pulled out into its own dependency-light package. It opens a `.pptx` as a bounded OPC/ZIP package, analyzes its slides into a JSON-friendly model, lets a caller scaffold and validate a fill plan, and applies that plan back into a new `.pptx` — covering text, tables, images, charts, notes, transitions, and SmartArt.

It has no dependency on the OpenAgent server, MCP layer, or any LLM/agent code — it's a pure library that any Go application can import directly.

## Layout

- [`*.go`](.) (root package `office`): the analyze → scaffold → check → fill pipeline that orchestrates template operations.
  - [`template_analyze.go`](template_analyze.go): parse a `.pptx` into a `model.Library` describing its slides and placeholders.
  - [`template_scaffold.go`](template_scaffold.go): build an empty `model.Plan` from a library for selected slides.
  - [`template_check.go`](template_check.go): validate a filled plan against the library before applying it.
  - [`template_apply.go`](template_apply.go): apply a plan to produce the output `.pptx`.
  - [`template_chart.go`](template_chart.go), [`template_image.go`](template_image.go), [`template_text.go`](template_text.go), [`template_notes.go`](template_notes.go), [`template_transition.go`](template_transition.go), [`template_layout.go`](template_layout.go), [`template_clone.go`](template_clone.go): per-feature handling used by the analyze/apply pipeline.
- [`ooxml/`](ooxml): lower-level OPC ZIP package access — bounded allocator, content types, relationships, XML tree helpers, atomic file writes (Unix/Windows variants).
- [`model/`](model): the JSON DTOs (`Library`, `Plan`, `CheckReport`, `ApplyOptions`, etc.) shared between the analyze, check, and apply stages.
- [`smartart/`](smartart): SmartArt-specific graphics handling, isolated from the rest of the template pipeline.

## Install

```bash
go get github.com/the-open-agent/office-tool-use
```

## Usage

```go
import (
    office "github.com/the-open-agent/office-tool-use"
    "github.com/the-open-agent/office-tool-use/model"
    "github.com/the-open-agent/office-tool-use/ooxml"
)

// 1. Analyze an existing .pptx into a library of slides/placeholders.
library, err := office.AnalyzeFile("input.pptx", ooxml.DefaultLimits())

// 2. Scaffold an empty fill plan for the slides you want to populate.
plan := office.ScaffoldPlan(library, []int{0, 1, 2}, false)

// ... populate plan.Slides[i] with text/table/image/chart content ...

// 3. Validate the filled plan against the library.
report := office.CheckPlan(library, plan)

// 4. Apply the plan to produce the output .pptx.
report, err = office.FillFile("input.pptx", "output.pptx", plan, model.ApplyOptions{}, ooxml.DefaultLimits())
```

## Quick Check

```bash
go test ./...
```

## License

[Apache License 2.0](LICENSE)
