-- Rewrite links between Markdown pages to the rendered HTML pages (gfm adapter, cc-guide).
-- page.md -> page.html, page.md#anchor -> page.html#anchor; external links stay as they are.
function Link(el)
  local target = el.target
  if target:match("^%a[%w+.-]*:") or target:match("^#") then
    return el
  end
  target = target:gsub("%.md$", ".html"):gsub("%.md#", ".html#")
  el.target = target
  return el
end
