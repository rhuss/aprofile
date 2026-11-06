-- Pandoc filter for the print document of a GitHub Markdown guide (gfm adapter, cc-guide).
-- Metadata: guide_root (absolute project path) and page_path (the page, relative to the
-- project root, for example docs/admin/install.md).
--   * the page's first h1 gets id <key with / as -> and data-guide-path=<key>, where the
--     key is the page path without docs/ and .md (admin/install, internal/notes), so
--     tools/guide/print-pdf.mjs can find every page and turn links into anchors
--   * links between pages become project-relative keys (user/start.md#anchor)
--   * images get absolute paths, so the print document can live anywhere
--   * IMPORTANT alerts that start with "Experimental:" are marked as experimental labels
local root, page_path = "", ""

local function normalize(path)
  local parts = {}
  for part in path:gmatch("[^/]+") do
    if part == ".." then
      table.remove(parts)
    elseif part ~= "." then
      table.insert(parts, part)
    end
  end
  return table.concat(parts, "/")
end

local function dirname(path)
  return path:match("^(.*)/[^/]*$") or ""
end

local function key_of(path)
  return (path:gsub("^docs/", ""):gsub("%.md$", ""))
end

local function is_relative(target)
  return not target:match("^%a[%w+.-]*:") and not target:match("^#") and not target:match("^/")
end

local function meta(m)
  root = pandoc.utils.stringify(m.guide_root or "")
  page_path = pandoc.utils.stringify(m.page_path or "")
end

local seen_title = false

local function header(el)
  if el.level == 1 and not seen_title then
    seen_title = true
    local key = key_of(page_path)
    el.identifier = key:gsub("/", "-")
    el.classes:insert("guide-page-title")
    el.attributes["guide-path"] = key
  end
  return el
end

local function link(el)
  if not is_relative(el.target) then
    return el
  end
  local path, frag = el.target:match("^([^#]*)(#?.*)$")
  local resolved = normalize(dirname(page_path) .. "/" .. path)
  if resolved:match("%.md$") then
    el.target = resolved:gsub("^docs/", "") .. frag
  end
  return el
end

local function image(el)
  if is_relative(el.src) then
    el.src = root .. "/" .. normalize(dirname(page_path) .. "/" .. el.src)
  end
  return el
end

local function div(el)
  if el.classes:includes("important") then
    local text = pandoc.utils.stringify(el)
    if text:match("^%s*Important%s*Experimental:") or text:match("^%s*Experimental:") then
      el.classes:insert("markdown-alert")
      el.classes:insert("experimental")
    end
  end
  return el
end

return {
  { Meta = meta },
  { Header = header, Link = link, Image = image, Div = div },
}
