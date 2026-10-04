#!/usr/bin/env python3
"""Check internal links in a built Hugo site.

Every href/src that points inside the site must resolve to a generated file,
every #fragment must name an id in the target page, and no page may repeat an
id. External links are not fetched. Usage: check-links.py site/public
"""
import sys
from html.parser import HTMLParser
from pathlib import Path
from urllib.parse import unquote, urlsplit


class Page(HTMLParser):
    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.ids = []
        self.links = []

    def handle_starttag(self, tag, attrs):
        a = dict(attrs)
        if a.get("id"):
            self.ids.append(a["id"])
        if tag == "a" and a.get("name"):
            self.ids.append(a["name"])
        for key in ("href", "src"):
            if a.get(key) and not (tag == "link" and a.get("rel") in ("canonical",)):
                self.links.append(a[key])


def target_file(root, page, path):
    if path.startswith("/"):
        f = root / path.lstrip("/")
    else:
        f = page.parent / path
    if path.endswith("/") or f.is_dir():
        f = f / "index.html"
    return f.resolve()


def main():
    root = Path(sys.argv[1] if len(sys.argv) > 1 else "site/public").resolve()
    pages = {}
    problems = []
    for f in sorted(root.rglob("*.html")):
        text = f.read_text(encoding="utf-8")
        if "HAHAHUGOSHORTCODE" in text:
            # A shortcode inside a heading leaks Hugo's placeholder into the TOC.
            problems.append(f"{f.relative_to(root)}: unexpanded shortcode placeholder")
        p = Page()
        p.feed(text)
        pages[f.resolve()] = p
    for f, p in pages.items():
        rel = f.relative_to(root)
        seen = set()
        for i in p.ids:
            if i in seen:
                problems.append(f"{rel}: duplicate id {i!r}")
            seen.add(i)
        for link in p.links:
            u = urlsplit(link)
            if u.scheme or u.netloc or link.startswith(("mailto:", "data:")):
                continue
            path = unquote(u.path)
            target = f if path == "" else target_file(root, f, path)
            if not target.exists():
                problems.append(f"{rel}: broken link {link!r}")
                continue
            if u.fragment and target in pages:
                if unquote(u.fragment) not in pages[target].ids:
                    problems.append(f"{rel}: missing anchor {link!r}")
    for line in problems:
        print(line)
    print(f"checked {len(pages)} pages: {len(problems)} problem(s)")
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main())
