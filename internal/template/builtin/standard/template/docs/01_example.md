# Introduction

This is a sample document for the `DocumentArtefact` kind. It demonstrates a table of contents, chapter numbering, and LaTeX formulas.

## Why a Second Rendering System?

`DocumentArtefact` renders Markdown files to PDF independently of the DataSource/DataSet/LayoutPage pipeline the rest of this bundle uses. Every `.md` file in this folder becomes a chapter — see `reports/document.yaml`.

### An Example of Mathematics

The area of a circle with radius $r$ is:

$$A = \pi r^2$$

### Chart Reference

:ref[ChartStructure:example_chart]{caption="Table 1"}
