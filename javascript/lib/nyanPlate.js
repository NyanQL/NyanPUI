console.log("loaded nyanPlate.js.");

let nyanPlateScript = {
    // 文字列の置換
    setString: function(htmlSegment, contextData) {
        return htmlSegment.replace(
            /(<\w+[^>]*data-nyan(?:String|Html)="(\w+)"[^>]*>)(?:(<!--\s*([^<]+?)\s*-->)|([^<]*))(<\/\w+>)/g,
            function(match, openTag, key, commentWrapper, commentContent, plainText, closeTag) {
                var replacement = contextData[key] !== undefined ? contextData[key] : (commentContent || plainText);
                return openTag + replacement + closeTag;
            }
        );
    },



    // 再帰対応ループ処理（ネスト対応）
    processLoop: function(htmlSegment, contextData) {
        return htmlSegment.replace(
            /(<(\w+)[^>]*data-nyanLoop="(\w+)"[^>]*>)([\s\S]*?)(<\/\2>)/gi,
            function(match, openTag, tagName, loopKey, innerTemplate, closeTag) {
                let items = contextData[loopKey];
                if (!items || !Array.isArray(items)) return match;
                let loopResult = "";

                items.forEach(function(item) {
                    let processed = innerTemplate;

                    // 🔁 再帰的にループを処理（ネスト対応）
                    processed = nyanPlateScript.processLoop(processed, item);

                    // 通常の属性処理
                    processed = nyanPlateScript.setString(processed, item);

                    processed = nyanPlateScript.setClass(processed, item);
                    processed = nyanPlateScript.setStyle(processed, item);
                    processed = nyanPlateScript.setHref(processed, item);
                    processed = nyanPlateScript.setId(processed, item);
                    processed = nyanPlateScript.setChecked(processed, item);
                    processed = nyanPlateScript.setSelected(processed, item);
                    processed = nyanPlateScript.setDisabled(processed, item);
                    processed = nyanPlateScript.setValue(processed, item);
                    processed = nyanPlateScript.setName(processed, item);
                    processed = nyanPlateScript.setSrc(processed, item);
                    processed = nyanPlateScript.setAlt(processed, item);

                    processed = nyanPlateScript.markAsDone(processed);

                    loopResult += processed;
                });

                return openTag + loopResult + closeTag;
            }
        );
    },

    setClass: function(htmlSegment, contextData) {
        return htmlSegment.replace(/data-nyanClass="(\w+)"/g, function(match, key) {
            return 'class="' + (contextData[key] !== undefined ? contextData[key] : "") + '" data-nyanDoneClass="' + key + '"';
        });
    },

    setStyle: function(htmlSegment, contextData) {
        return htmlSegment.replace(/data-nyanStyle="(\w+)"/g, function(match, key) {
            return 'style="' + (contextData[key] !== undefined ? contextData[key] : "") + '" data-nyanDoneStyle="' + key + '"';
        });
    },

    setHref: function(htmlSegment, contextData) {
        return htmlSegment.replace(/data-nyanHref="(\w+)"/g, function(match, key) {
            return 'href="' + (contextData[key] !== undefined ? contextData[key] : "") + '" data-nyanDoneHref="' + key + '"';
        });
    },

    setId: function(htmlSegment, contextData) {
        return htmlSegment.replace(/data-nyanId="(\w+)"/g, function(match, key) {
            return 'id="' + (contextData[key] !== undefined ? contextData[key] : "") + '" data-nyanDoneId="' + key + '"';
        });
    },

    setChecked: function(htmlSegment, contextData) {
        return htmlSegment.replace(/data-nyanChecked="(\w+)"/g, function(match, key) {
            let val = contextData[key];
            if (val === true || val === 'checked') return 'checked data-nyanDoneChecked="' + key + '"';
            return 'data-nyanDoneChecked="' + key + '"';
        });
    },

    setSelected: function(htmlSegment, contextData) {
        return htmlSegment.replace(/data-nyanSelected="(\w+)"/g, function(match, key) {
            let val = contextData[key];
            if (val === true || val === 'selected') return 'selected data-nyanDoneSelected="' + key + '"';
            return 'data-nyanDoneSelected="' + key + '"';
        });
    },

    setDisabled: function(htmlSegment, contextData) {
        return htmlSegment.replace(/data-nyanDisabled="(\w+)"/g, function(match, key) {
            let val = contextData[key];
            if (val === true || val === 'disabled') return 'disabled data-nyanDoneDisabled="' + key + '"';
            return 'data-nyanDoneDisabled="' + key + '"';
        });
    },

    setValue: function(htmlSegment, contextData) {
        return htmlSegment.replace(/data-nyanValue="(\w+)"/g, function(match, key) {
            return 'value="' + (contextData[key] !== undefined ? contextData[key] : "") + '" data-nyanDoneValue="' + key + '"';
        });
    },

    setName: function(htmlSegment, contextData) {
        return htmlSegment.replace(/data-nyanName="(\w+)"/g, function(match, key) {
            return 'name="' + (contextData[key] !== undefined ? contextData[key] : "") + '" data-nyanDoneName="' + key + '"';
        });
    },

    setSrc: function(htmlSegment, contextData) {
        return htmlSegment.replace(/data-nyanSrc="(\w+)"/g, function(match, key) {
            return 'src="' + (contextData[key] !== undefined ? contextData[key] : "") + '" data-nyanDoneSrc="' + key + '"';
        });
    },

    setAlt: function(htmlSegment, contextData) {
        return htmlSegment.replace(/data-nyanAlt="(\w+)"/g, function(match, key) {
            return 'alt="' + (contextData[key] !== undefined ? contextData[key] : "") + '" data-nyanDoneAlt="' + key + '"';
        });
    },

    // 属性を変換済みにする
    markAsDone: function(htmlSegment) {
        let nyanAttrs = [
            "nyanString", "nyanHtml", "nyanClass", "nyanStyle", "nyanHref", "nyanId",
            "nyanChecked", "nyanSelected", "nyanDisabled", "nyanValue",
            "nyanName", "nyanSrc", "nyanAlt"
        ];
        nyanAttrs.forEach(function(attr) {
            let regex = new RegExp('(\\s)data-' + attr + '="([^"]*)"', 'gi');
            htmlSegment = htmlSegment.replace(regex, function(_, space, key) {
                return space + 'data-nyanDone' + attr.charAt(0).toUpperCase() + attr.slice(1) + '="' + key + '"';
            });
        });
        return htmlSegment;
    }
};

// 実行関数
function nyanPlate(data, htmlCode) {
    htmlCode = htmlCode || nyanHtmlCode;

    // ループ処理（ネスト対応）
    htmlCode = nyanPlateScript.processLoop(htmlCode, data);

    // 通常の属性置換
    htmlCode = nyanPlateScript.setString(htmlCode, data);

    htmlCode = nyanPlateScript.setClass(htmlCode, data);
    htmlCode = nyanPlateScript.setStyle(htmlCode, data);
    htmlCode = nyanPlateScript.setHref(htmlCode, data);
    htmlCode = nyanPlateScript.setId(htmlCode, data);
    htmlCode = nyanPlateScript.setChecked(htmlCode, data);
    htmlCode = nyanPlateScript.setSelected(htmlCode, data);
    htmlCode = nyanPlateScript.setDisabled(htmlCode, data);
    htmlCode = nyanPlateScript.setValue(htmlCode, data);
    htmlCode = nyanPlateScript.setName(htmlCode, data);
    htmlCode = nyanPlateScript.setSrc(htmlCode, data);
    htmlCode = nyanPlateScript.setAlt(htmlCode, data);

    htmlCode = nyanPlateScript.markAsDone(htmlCode);

    return htmlCode;
}
