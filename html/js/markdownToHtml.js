/**
 * Simple Markdown → HTML parser (no deps)
 * - Supports: headings, paragraphs, blockquotes, code fences, inline code,
 *   bold, italic, strikethrough, links, images, hr, unordered/ordered lists,
 *   tables (basic + alignment)
 * - Flat lists (nestingは簡易対応: 無視)
 * - Basic HTML escaping & URL sanitization
 */

(function (root, factory) {
    if (typeof module === "object" && module.exports) {
        module.exports = factory();
    } else {
        root.markdownToHtml = factory();
    }
})(typeof self !== "undefined" ? self : this, function () {
    "use strict";

    const escapeHtml = (str) =>
        String(str)
            .replace(/&/g, "&amp;")
            .replace(/</g, "&lt;")
            .replace(/>/g, "&gt;")
            .replace(/"/g, "&quot;")
            .replace(/'/g, "&#39;");

    // 許可するURLスキーム（http, https, mailto, data, 相対パス）
    function sanitizeUrl(url) {
        const trimmed = String(url).trim();
        try {
            const parsed = new URL(trimmed, "http://example.com"); // 相対URLも解決可能にする
            const scheme = parsed.protocol.replace(":", "");
            if (["http", "https", "mailto", "data"].includes(scheme)) {
                return trimmed;
            }
            return "#";
        } catch {
            // 相対URLや # は許可
            if (/^(\/|\.\/|\.\.\/|#)/.test(trimmed)) return trimmed;
            return "#";
        }
    }

    function tokenizeCodeFences(lines, i) {
        const open = lines[i];
        const m = open.match(/^```(\s*\w+)?\s*$/);
        if (!m) return null;

        const lang = (m[1] || "").trim();
        const codeLines = [];
        i++;
        while (i < lines.length && !/^```/.test(lines[i])) {
            codeLines.push(lines[i]);
            i++;
        }
        if (i < lines.length && /^```/.test(lines[i])) i++;

        const codeHtml =
            `<pre><code${lang ? ` class="language-${escapeHtml(lang)}"` : ""}>` +
            `${escapeHtml(codeLines.join("\n"))}</code></pre>`;

        return { nextIndex: i, html: codeHtml };
    }

    function collectList(lines, i) {
        const ulRe = /^\s*([-+*])\s+(.+)$/;
        const olRe = /^\s*(\d+)\.\s+(.+)$/;

        const first = lines[i];
        let isOrdered = false;
        let m = first.match(olRe) || first.match(ulRe);
        if (!m) return null;

        isOrdered = !!first.match(olRe);
        const items = [];

        while (i < lines.length) {
            const line = lines[i];
            const mm = isOrdered ? line.match(olRe) : line.match(ulRe);
            if (!mm) break;
            items.push(mm[2]);
            i++;
            while (i < lines.length && /^\s{2,}\S/.test(lines[i])) {
                items[items.length - 1] += " " + lines[i].trim();
                i++;
            }
        }

        return {
            nextIndex: i,
            html:
                (isOrdered ? "<ol>" : "<ul>") +
                items.map((t) => `<li>${applyInline(t)}</li>`).join("") +
                (isOrdered ? "</ol>" : "</ul>"),
        };
    }

    function collectBlockquote(lines, i) {
        if (!/^\s*>/.test(lines[i])) return null;
        const chunk = [];

        while (i < lines.length && /^\s*>/.test(lines[i])) {
            chunk.push(lines[i].replace(/^\s*>\s?/, ""));
            i++;
        }

        const htmlInside = chunk
            .map((ln) => (ln.trim() ? applyInline(ln) : ""))
            .join("<br>");

        return { nextIndex: i, html: `<blockquote>${htmlInside}</blockquote>` };
    }

    function collectTable(lines, i) {
        if (!/^\s*\|.+\|\s*$/.test(lines[i])) return null;

        const rows = [];
        while (i < lines.length && /^\s*\|.+\|\s*$/.test(lines[i])) {
            const row = lines[i].trim();
            const cells = row.replace(/^\|/, "").replace(/\|$/, "").split("|");
            rows.push(cells.map((c) => c.trim()));
            i++;
        }

        if (rows.length < 2) return null;

        const header = rows[0];
        const divider = rows[1];
        const body = rows.slice(2);

        // アライメント判定
        const aligns = divider.map((d) => {
            d = d.trim();
            if (/^:?-{3,}:?$/.test(d)) {
                if (/^:-+:$/.test(d)) return "center";
                if (/^-+:$/.test(d)) return "right";
                if (/^:-+$/.test(d)) return "left";
                return null;
            }
            return null;
        });

        const thead =
            "<thead><tr>" +
            header
                .map((h, idx) => {
                    const align = aligns[idx] ? ` style="text-align:${aligns[idx]}"` : "";
                    return `<th${align}>${applyInline(h)}</th>`;
                })
                .join("") +
            "</tr></thead>";

        const tbody =
            body.length > 0
                ? "<tbody>" +
                body
                    .map(
                        (cols) =>
                            "<tr>" +
                            cols
                                .map((c, idx) => {
                                    const align = aligns[idx]
                                        ? ` style="text-align:${aligns[idx]}"`
                                        : "";
                                    return `<td${align}>${applyInline(c)}</td>`;
                                })
                                .join("") +
                            "</tr>"
                    )
                    .join("") +
                "</tbody>"
                : "";

        return {
            nextIndex: i,
            html: `<table>${thead}${tbody}</table>`,
        };
    }

    function isHr(line) {
        return /^\s*(\*{3,}|-{3,}|_{3,})\s*$/.test(line);
    }

    function headingHtml(line) {
        const m = line.match(/^(#{1,6})\s+(.*)$/);
        if (!m) return null;
        const level = m[1].length;
        const text = m[2].trim();
        const slug = text
            .toLowerCase()
            .replace(/<\/?[^>]+(>|$)/g, "")
            .replace(/[^\w\s-]/g, "")
            .trim()
            .replace(/\s+/g, "-");
        return `<h${level} id="${escapeHtml(slug)}">${applyInline(text)}</h${level}>`;
    }

    function applyInline(str) {
        let s = escapeHtml(str);

        const codeBuckets = [];
        s = s.replace(/`([^`]+)`/g, (_, code) => {
            const token = `__CODE_TOKEN_${codeBuckets.length}__`;
            codeBuckets.push(`<code>${escapeHtml(code)}</code>`);
            return token;
        });

        // 画像（srcはエスケープしない）
        s = s.replace(/!\[([^\]]*)\]\(([^)]+)\)/g, (_, alt, url) => {
            const safeUrl = sanitizeUrl(url);
            return `<img alt="${escapeHtml(alt)}" src="${safeUrl}">`;
        });

        // リンク
        s = s.replace(/\[([^\]]+)\]\(([^)]+)\)/g, (_, text, url) => {
            const safeUrl = sanitizeUrl(url);
            return `<a href="${escapeHtml(safeUrl)}">${text}</a>`;
        });

        s = s.replace(/(\*\*|__)(.+?)\1/g, "<strong>$2</strong>");
        s = s.replace(/~~(.+?)~~/g, "<del>$1</del>");
        s = s.replace(/(^|[^\*])\*([^*\n]+)\*(?!\*)/g, "$1<em>$2</em>");
        s = s.replace(/(^|[^_])_([^_\n]+)_(?!_)/g, "$1<em>$2</em>");

        s = s.replace(/__CODE_TOKEN_(\d+)__/g, (_, idx) => codeBuckets[Number(idx)]);

        return s;
    }

    function markdownToHtml(md) {
        if (typeof md !== "string") md = String(md ?? "");
        const lines = md.replace(/\r\n?/g, "\n").split("\n");

        const out = [];
        let i = 0;
        let para = [];

        const flushPara = () => {
            if (!para.length) return;
            const text = para.join(" ").trim();
            if (text) out.push(`<p>${applyInline(text)}</p>`);
            para = [];
        };

        while (i < lines.length) {
            const line = lines[i];

            if (!line.trim()) {
                flushPara();
                i++;
                continue;
            }

            const codeTok = tokenizeCodeFences(lines, i);
            if (codeTok) {
                flushPara();
                out.push(codeTok.html);
                i = codeTok.nextIndex;
                continue;
            }

            if (isHr(line)) {
                flushPara();
                out.push("<hr>");
                i++;
                continue;
            }

            const h = headingHtml(line);
            if (h) {
                flushPara();
                out.push(h);
                i++;
                continue;
            }

            const bq = collectBlockquote(lines, i);
            if (bq) {
                flushPara();
                out.push(bq.html);
                i = bq.nextIndex;
                continue;
            }

            const list = collectList(lines, i);
            if (list) {
                flushPara();
                out.push(list.html);
                i = list.nextIndex;
                continue;
            }

            const table = collectTable(lines, i);
            if (table) {
                flushPara();
                out.push(table.html);
                i = table.nextIndex;
                continue;
            }

            para.push(line.trim());
            i++;
        }

        flushPara();
        return out.join("\n");
    }

    return markdownToHtml;
});
