# CyberGuard project report

[`main.tex`](main.tex) is the English technical report for the whole project.
It covers all GitHub branches reviewed for this edition. The integrated
baseline is `da6c4b8` on `main`; the unmerged sign-up and AI UI changes at
`e771a27` are documented separately.

Read the [compiled PDF](CyberGuard-Technical-Report.pdf) without installing LaTeX.
For installation and upgrade procedures, use the [project README](../../README.md)
and its linked runbooks.

## Build the PDF

With `latexmk` and a TeX Live installation that provides XeLaTeX, Latin Modern,
PGF/TikZ and `pdflscape`, run from the repository root:

```sh
mkdir -p /tmp/cyberguard-report
latexmk -xelatex -interaction=nonstopmode -halt-on-error \
  -outdir=/tmp/cyberguard-report docs/report/main.tex
```

Alternatively, with [Tectonic](https://tectonic-typesetting.github.io/):

```sh
mkdir -p /tmp/cyberguard-report
tectonic --outdir /tmp/cyberguard-report docs/report/main.tex
```

The output is `/tmp/cyberguard-report/main.pdf`. It embeds both original
[BPMN diagrams](../bpmn/) in a landscape appendix. Tectonic may download TeX
resources on the first build. When publishing a report update, replace
`CyberGuard-Technical-Report.pdf` with the new build; do not commit auxiliary files.
