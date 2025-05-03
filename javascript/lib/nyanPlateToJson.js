function nyanPlateToJson(htmlString) {
    function extractAttributes(tag) {
        let attrs = {};
        let attrRegex = /([\w\-:]+)="([^"]*)"/g;
        let match;
        while ((match = attrRegex.exec(tag))) {
            attrs[match[1]] = match[2];
        }
        return attrs;
    }

    function parseHTML(html) {
        let stack = [];
        let root = { children: [] };
        let current = root;
        let tagRegex = /<\/?[^>]+>/g;
        let lastIndex = 0;
        let match;

        function createNode(tag, isClosing) {
            let isSelfClosing = /\/>$/.test(tag) || /^<\w+[^>]*\/>$/.test(tag);
            let tagNameMatch = tag.match(/^<\/?([\w\-]+)/);
            let tagName = tagNameMatch ? tagNameMatch[1] : null;
            let attributes = extractAttributes(tag);
            return {
                tag: tagName,
                attributes: attributes,
                children: [],
                parent: null,
                text: '',
                selfClosing: isSelfClosing,
                closing: isClosing
            };
        }

        while ((match = tagRegex.exec(html))) {
            let textContent = html.substring(lastIndex, match.index);
            if (textContent.trim()) {
                current.children.push({ text: textContent });
            }

            let tagText = match[0];
            let isClosing = /^<\//.test(tagText);
            let node = createNode(tagText, isClosing);

            if (isClosing) {
                if (stack.length > 0) {
                    current = stack.pop();
                }
            } else {
                node.parent = current;
                current.children.push(node);
                if (!node.selfClosing) {
                    stack.push(current);
                    current = node;
                }
            }

            lastIndex = match.index + tagText.length;
        }

        return root;
    }

    function getText(node) {
        let buffer = '';
        if (!node.children) return '';
        for (let i = 0; i < node.children.length; i++) {
            let child = node.children[i];
            if (child.text) buffer += child.text;
            else buffer += getText(child);
        }
        return buffer.trim();
    }

    function buildChildren(node) {
        let result = {};
        if (!node.children) return result;
        for (let i = 0; i < node.children.length; i++) {
            let child = node.children[i];
            if (child.text) continue;
            let childJSON = buildJSON(child);
            for (let key in childJSON) {
                result[key] = childJSON[key];
            }
        }
        return result;
    }

    function buildJSON(node) {
        let result = {};

        if (node.attributes) {
            let attrs = node.attributes;

            if (attrs['data-nyanDoneNyanString']) {
                result[attrs['data-nyanDoneNyanString']] = getText(node);
            }
            if (attrs['data-nyanDoneStyle']) {
                result[attrs['data-nyanDoneStyle']] = attrs.style || '';
            }
            if (attrs['data-nyanDoneClass']) {
                result[attrs['data-nyanDoneClass']] = attrs['class'] || '';
            }
            if (attrs['data-nyanDoneNyanHtml']) {
                result[attrs['data-nyanDoneNyanHtml']] = buildChildren(node);
                return result;
            }
            if (attrs['data-nyanLoop']) {
                let arr = [];
                let children = node.children || [];
                for (let i = 0; i < children.length; i++) {
                    let itemNode = children[i];
                    let item = buildJSON(itemNode);
                    if (typeof item === 'object') arr.push(item);
                }
                result[attrs['data-nyanLoop']] = arr;
                return result;
            }
            if (attrs['data-nyanDoneFor']) {
                result[attrs['data-nyanDoneFor']] = attrs['for'] || '';
            }
        }

        let childResult = buildChildren(node);
        for (let key in childResult) {
            result[key] = childResult[key];
        }

        return result;
    }

    // 実行部
    let tree = parseHTML(htmlString);
    return buildJSON(tree);
}
