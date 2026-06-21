# office-tool-use

Standalone Go implementation for bounded OOXML/PPTX package handling and
PowerPoint template operations extracted from OpenAgent.

## Layout

- `*.go`: root package with bounded OOXML ZIP package handling, content types,
  relationships, XML tree helpers, and generic PPTX template analysis/fill
  workflow.
- `template_*.go`: PowerPoint template analysis, validation, scaffolding,
  filling, chart/image/text/table/notes/transition handling.
- `smartart/`: SmartArt-specific implementation files only. No fixtures,
  tests, or markdown notes live here.

## Quick Check

```bash
go test ./...
```

The package is intentionally independent from the OpenAgent tool/MCP layer.
Callers can wrap `AnalyzeFile`, `ScaffoldPlan`, `CheckPlan`, and `FillFile`
from their own application boundary.
