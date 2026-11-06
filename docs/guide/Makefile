# Guide build, GitHub Markdown adapter (cc-guide). The pages under docs/ and internal/
# render on GitHub as they are. Baseline values live in generated regions
# (<!-- guide:attr NAME -->VALUE<!-- /guide:attr -->) that `make site` rewrites from
# baseline.yaml; pages never type versions or commits.
# `make site` also renders the pages with pandoc into build/site, so the leak check,
# the packages, and the PDF work from a built view like in the other adapters.
# Targets and their semantics are identical across adapters (contracts/adapter.md).
SHELL := /bin/bash
UV ?= uv
TOOLS := tools/guide
STRICT ?= 0
VIEW ?= shareable
PORT ?= 8642
# make preview opens the browser once the server answers; OPEN=0 only serves
# (the preview skill opens it itself).
OPEN ?= 1
OPEN_LATER = if [ "$(OPEN)" != 0 ]; then ( for i in $$(seq 1 60); do \
		if curl -fsS -o /dev/null http://127.0.0.1:$(PORT)/ 2>/dev/null; then \
			(open http://127.0.0.1:$(PORT)/$$start || xdg-open http://127.0.0.1:$(PORT)/$$start) >/dev/null 2>&1; break; fi; \
		sleep 0.5; done ) & fi

GET = $(UV) run --quiet $(TOOLS)/guidecfg.py get
# The PDF file for a view (shareable or internal), as the shell variable out.
PDF_OUT = slug=$$($(GET) slug) && date=$$($(GET) baseline.captured) || exit 1; \
	if [ "$(1)" = internal ]; then out=dist/$$slug-$$date-INTERNAL.pdf; else out=dist/$$slug-$$date.pdf; fi
# The print document (pandoc/print.sh): navigation sections are h1.guide-section,
# every page starts with h1.guide-page-title.
PRINT_ARGS = --page-heading 'h1.guide-page-title' --section-heading 'h1.guide-section'

.PHONY: help doctor fetch-baseline regions site site-internal check preview pdf print-pdf dist drift clean

help:
	@echo "Guide targets (GitHub Markdown adapter):"
	@echo "  make doctor          check the tool chain"
	@echo "  make fetch-baseline  fetch the pinned sources into .guide/cache/"
	@echo "  make regions         rewrite the baseline values in docs/ and internal/"
	@echo "  make site            regions, then render docs/ into build/site (pandoc)"
	@echo "  make site-internal   render docs/ and internal/ into build/site-internal"
	@echo "  make check           builds, page and content checks, leak check (STRICT=1 adds the claim ledger)"
	@echo "  make preview         serve the Markdown with live reload (VIEW=internal for internal/, PORT=$(PORT))"
	@echo "  make pdf             one PDF of the guide (VIEW=internal for the internal view)"
	@echo "  make dist            check, then the PDF(s) and the shareable tarball (and the internal ones when enabled)"
	@echo "  make drift           compare the baseline with the sources (exit 10 on drift)"

node_modules:
	npm install

doctor:
	$(UV) run --quiet $(TOOLS)/doctor.py

fetch-baseline:
	$(UV) run --quiet $(TOOLS)/fetch.py fetch

regions:
	@dirs=docs; if [ -d internal ]; then dirs="docs internal"; fi; \
	$(UV) run --quiet $(TOOLS)/attrs.py regions --write $$dirs

site: regions
	bash pandoc/render.sh build/site docs:.

site-internal: regions
	@if [ "$$($(GET) internal.enabled)" != true ]; then \
		echo "internal part disabled in guide.yaml; skipping site-internal"; \
	else \
		bash pandoc/render.sh build/site-internal docs:docs internal:internal; \
	fi

check: fetch-baseline site site-internal
	$(UV) run --quiet $(TOOLS)/guidecfg.py validate
	@dirs=docs; if [ -d internal ]; then dirs="docs internal"; fi; \
	$(UV) run --quiet $(TOOLS)/attrs.py regions $$dirs
	STRICT=$(STRICT) $(UV) run --quiet $(TOOLS)/check.py
	@if [ "$(STRICT)" = 1 ]; then $(UV) run --quiet $(TOOLS)/ledger.py; fi
	$(UV) run --quiet $(TOOLS)/leakcheck.py build/site

preview:
	@# The internal view serves the project root, so internal pages can link to docs/.
	@if [ "$(VIEW)" = internal ]; then dir=.; start=internal/README.md; else dir=docs; start=; fi; \
	$(MAKE) --no-print-directory regions && \
	echo "Serving $$dir on http://127.0.0.1:$(PORT)/$$start with live reload (Ctrl-C to stop)" && \
	{ $(OPEN_LATER); } && \
	npx --no-install markserv $$dir --address 127.0.0.1 --port $(PORT) --no-browser --light

pdf:
	@$(call PDF_OUT,$(VIEW)); rm -f "$$out"
	@$(MAKE) --no-print-directory check
	@$(MAKE) --no-print-directory print-pdf VIEW=$(VIEW)

# Assemble the print document (pages in docs/SUMMARY.md order) and print it; use make pdf.
print-pdf: node_modules
	@$(call PDF_OUT,$(VIEW)); \
	if [ "$(VIEW)" = internal ]; then \
		if [ "$$($(GET) internal.enabled)" != true ]; then echo "internal part disabled in guide.yaml; no internal PDF" >&2; exit 1; fi; \
		view=internal; \
	else \
		view=shareable; \
	fi; \
	html=build/print-$$view/print.html; \
	bash pandoc/print.sh $$view "$$html" || exit 1; \
	mkdir -p dist; rm -f "$$out"; \
	node $(TOOLS)/print-pdf.mjs --html "$$html" --guide-root . --view $$view --out "$$out" $(PRINT_ARGS) || exit 1; \
	if [ $$view = shareable ]; then \
		$(UV) run --quiet $(TOOLS)/leakcheck.py "$$out" || { rm -f "$$out"; echo "leak check failed; $$out removed" >&2; exit 1; }; \
	else \
		echo "WARNING: $$out contains internal content. Do not share it outside the internal audience."; \
	fi

dist: check
	@$(MAKE) --no-print-directory print-pdf VIEW=shareable
	@if [ "$$($(GET) internal.enabled)" = true ]; then $(MAKE) --no-print-directory print-pdf VIEW=internal; fi
	@slug=$$($(GET) slug) && date=$$($(GET) baseline.captured) || exit 1; \
	name=$$slug-$$date; \
	rm -rf dist/stage dist/$$name.tar.gz dist/$$name-INTERNAL.tar.gz && mkdir -p dist/stage && \
	cp -R build/site dist/stage/$$name && \
	tar -C dist/stage -czf dist/$$name.tar.gz $$name && \
	echo "Created dist/$$name.tar.gz (shareable)" || exit 1; \
	if [ "$$($(GET) internal.enabled)" = true ]; then \
		cp -R build/site-internal dist/stage/$$name-INTERNAL && \
		tar -C dist/stage -czf dist/$$name-INTERNAL.tar.gz $$name-INTERNAL && \
		echo "WARNING: dist/$$name-INTERNAL.tar.gz contains internal content. Do not share it outside the internal audience." || exit 1; \
	fi; \
	rm -rf dist/stage

drift:
	$(UV) run --quiet $(TOOLS)/drift.py --check

clean:
	rm -rf build dist
