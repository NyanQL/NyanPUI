/**
 * nyanPlateToJson  (ECMAScript 5.1 compatible)
 *  - nyanPlate が生成した data-nyanDone～ / data-nyanLoop を JSON に戻す
 *  - 14 種類すべての属性に対応
 */
function nyanPlateToJson(htmlString) {

    /* ========= 汎用ヘルパ ========= */

    // タグ文字列から属性を抽出（値が無ければ true）
    function extractAttributes(tagText) {
        var attrs = {};
        var attrRe = /([\w\-:]+)(?:="([^"]*)")?/g, m;
        while ((m = attrRe.exec(tagText))) {
            attrs[m[1]] = (m[2] !== undefined) ? m[2] : true;
        }
        return attrs;
    }

    // ごく簡単な DOM ツリー構築
    function parseHTML(html) {
        var root   = { children: [] };
        var stack  = [];
        var curr   = root;
        var tagRe  = /<\/?[^>]+>/g, last = 0, m;

        function makeNode(tagText, closing) {
            var selfClosing = /\/>$/.test(tagText);
            var nameMatch   = tagText.match(/^<\/?([\w\-]+)/);
            return {
                tag:       nameMatch ? nameMatch[1] : '',
                attributes: extractAttributes(tagText),
                children:   [],
                selfClosing: selfClosing,
                closing:     closing,
                parent:      null,
                text:        ''
            };
        }

        while ((m = tagRe.exec(html))) {
            // テキストノード
            if (m.index > last) {
                var txt = html.slice(last, m.index);
                if (txt.replace(/\s+/g, '').length) {   // 空白のみは無視
                    curr.children.push({ text: txt });
                }
            }

            var tagText = m[0];
            if (/^<\//.test(tagText)) {                  // 閉じタグ
                curr = stack.pop() || root;
            } else {                                     // 開きタグ
                var node = makeNode(tagText, false);
                node.parent = curr;
                curr.children.push(node);
                if (!node.selfClosing) {
                    stack.push(curr);
                    curr = node;
                }
            }
            last = tagRe.lastIndex;
        }
        return root;
    }

    // ノード配下のテキストを連結
    function collectText(node) {
        if (!node.children) { return ''; }
        var buf = '';
        for (var i = 0; i < node.children.length; i++) {
            var c = node.children[i];
            buf += c.text ? c.text : collectText(c);
        }
        return buf.trim();
    }

    // オブジェクト同士をマージ（ES5.1 用）
    function mergeInto(dest, src) {
        for (var k in src) { if (src.hasOwnProperty(k)) dest[k] = src[k]; }
        return dest;
    }

    // 子要素の JSON をまとめて取得
    function childrenToJSON(node) {
        var combined = {};
        if (!node.children) { return combined; }

        for (var i = 0; i < node.children.length; i++) {
            var c = node.children[i];
            if (c.text) { continue; }
            mergeInto(combined, buildJSON(c));
        }
        return combined;
    }

    /* ========= 本体 ========= */

    function buildJSON(node) {
        var out = {};
        var a   = node.attributes || {};

        /* 文字列 / HTML / ループ */
        if (a['data-nyanDoneNyanString']) {
            out[a['data-nyanDoneNyanString']] = collectText(node);
        }
        if (a['data-nyanDoneNyanHtml']) {
            out[a['data-nyanDoneNyanHtml']] = childrenToJSON(node);
            return out;                                  // ここで確定
        }
        if (a['data-nyanLoop']) {
            var arr = [];
            for (var i = 0; i < (node.children || []).length; i++) {
                var c = node.children[i];
                if (c.text) { continue; }
                arr.push(buildJSON(c));
            }
            out[a['data-nyanLoop']] = arr;
            return out;                                  // ここで確定
        }

        /* 単純属性 */
        var simple = [
            ['data-nyanDoneClass',  'class'],
            ['data-nyanDoneStyle',  'style'],
            ['data-nyanDoneHref',   'href'],
            ['data-nyanDoneId',     'id'],
            ['data-nyanDoneValue',  'value'],
            ['data-nyanDoneName',   'name'],
            ['data-nyanDoneSrc',    'src'],
            ['data-nyanDoneAlt',    'alt'],
            ['data-nyanDoneFor',    'for']
        ];
        for (var s = 0; s < simple.length; s++) {
            var doneAttr = simple[s][0], realAttr = simple[s][1];
            if (a[doneAttr]) {
                out[a[doneAttr]] = (a[realAttr] !== undefined) ? a[realAttr] : '';
            }
        }

        /* boolean 属性 */
        var bools = [
            'data-nyanDoneChecked',
            'data-nyanDoneSelected',
            'data-nyanDoneDisabled'
        ];
        for (var b = 0; b < bools.length; b++) {
            var ba = bools[b];
            if (a[ba]) { out[a[ba]] = true; }
        }

        /* 子要素を統合 */
        mergeInto(out, childrenToJSON(node));
        return out;
    }

    /* ========= 実行 ========= */
    return buildJSON(parseHTML(htmlString));
}
