function nyanPlateToJson(html) {
    let result = {};

    // 1. ループ処理
    let loopRegex = /<(\w+)[^>]*data-nyanLoop="(\w+)"[^>]*>([\s\S]*?)<\/\1>/g;
    let match;

    while ((match = loopRegex.exec(html)) !== null) {
        let loopKey = match[2];
        let loopContent = match[3];

        let rowRegex = /<(\w+)[^>]*>([\s\S]*?)<\/\1>/g;
        let rowMatch;
        let items = [];

        while ((rowMatch = rowRegex.exec(loopContent)) !== null) {
            let rowHtml = rowMatch[0];
            let item = {};

            let attrTagRegex = /<(\w+)([^>]*)>([^<]*)<\/\1>/g;
            let singleTagMatch;

            while ((singleTagMatch = attrTagRegex.exec(rowHtml)) !== null) {
                let tagAttrs = singleTagMatch[2];
                let tagContent = singleTagMatch[3];

                let attrRegex = /(\w[\w\-]*)="([^"]*)"/g;
                let attrMatch;
                let attrs = {};
                while ((attrMatch = attrRegex.exec(tagAttrs)) !== null) {
                    attrs[attrMatch[1]] = attrMatch[2];
                }

                Object.keys(attrs).forEach(function(attrName) {
                    let doneMatch = attrName.match(/^data-nyanDone([A-Z]\w+)$/);
                    if (doneMatch) {
                        let key = attrs[attrName];
                        let htmlAttr = doneMatch[1];
                        htmlAttr = htmlAttr.charAt(0).toLowerCase() + htmlAttr.slice(1);

                        if (htmlAttr === "nyanString") {
                            item[key] = tagContent;
                        } else if (htmlAttr === 'checked' || htmlAttr === 'disabled' || htmlAttr === 'selected') {
                            item[key] = tagAttrs.indexOf(htmlAttr) !== -1 ? htmlAttr : "";
                        } else if (attrs.hasOwnProperty(htmlAttr)) {
                            item[key] = attrs[htmlAttr];
                        } else {
                            item[key] = "";
                        }
                    }
                });
            }

            if (Object.keys(item).length > 0) {
                items.push(item);
            }
        }

        if (items.length > 0) {
            result[loopKey] = items;
        }
    }

    // 2. 非ループ部分（ループ除去後に処理）
    let cleanedHtml = html.replace(loopRegex, "");
    let tagWithTextRegex = /<(\w+)([^>]*)>([^<]*)<\/\1>/g;

    while ((match = tagWithTextRegex.exec(cleanedHtml)) !== null) {
        let attrString = match[2];
        let innerText = match[3];

        let attrRegex = /(\w[\w\-]*)="([^"]*)"/g;
        let attrMatch;
        let attrs = {};

        while ((attrMatch = attrRegex.exec(attrString)) !== null) {
            attrs[attrMatch[1]] = attrMatch[2];
        }

        Object.keys(attrs).forEach(function(attrName) {
            let doneMatch = attrName.match(/^data-nyanDone([A-Z]\w+)$/);
            if (doneMatch) {
                let key = attrs[attrName];
                let htmlAttr = doneMatch[1];
                htmlAttr = htmlAttr.charAt(0).toLowerCase() + htmlAttr.slice(1);

                if (htmlAttr === "nyanString") {
                    result[key] = innerText;
                } else if (htmlAttr === 'checked' || htmlAttr === 'disabled' || htmlAttr === 'selected') {
                    result[key] = attrString.indexOf(htmlAttr) !== -1 ? htmlAttr : "";
                } else if (attrs.hasOwnProperty(htmlAttr)) {
                    result[key] = attrs[htmlAttr];
                } else {
                    result[key] = "";
                }
            }
        });
    }

    return result;
}
