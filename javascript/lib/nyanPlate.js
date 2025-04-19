/*
にゃんぷれーと
文字列の置き換え data-nyanString="key"
ループ処理 data-nyanLoop ="key"
htmlタグへのclassの指定 data-nyanClass = "key"
htmlタグへのstyleの指定 data-nyanStyle = "key"
リンクの指定 data-nyanHref = "key"
idの指定 data-nyanId = "key"
checked指定 data-nyanChecked = "key"
selected指定 data-nyanSelected = "key"
disabled指定 data-nyanDisabled = "key"
value指定 data-nyanValue = "key"
name指定 data-nyanName = "key"
処理の実行は nyanPlate(data, htmlCode);
*/
console.log("loaded nyanPlate.js.");

const nyanPlateScript = {
    // 文字列の置換
    setString: function(htmlSegment, contextData) {
        return htmlSegment.replace(
            /(<\w+[^>]*data-nyanString="(\w+)"[^>]*>)(?:(<!--\s*([^<]+?)\s*-->)|([^<]*))(<\/\w+>)/g,
            function(match, openTag, key, commentWrapper, commentContent, plainText, closeTag) {
                var replacement = contextData[key] !== undefined ? contextData[key] : (commentContent || plainText);
                return openTag + replacement + closeTag;
            }
        );
    },

    // ループ処理
    processLoop: function(htmlSegment, contextData) {
        return htmlSegment.replace(
            /(<(\w+)[^>]*data-nyanLoop="(\w+)"[^>]*>)([\s\S]*?)(<\/\2>)/gi,
            function(match, openTag, tagName, loopKey, innerTemplate, closeTag) {
                var items = contextData[loopKey];
                if (!items || !Array.isArray(items)) return match;
                var loopResult = "";
                items.forEach(function(item) {
                    var processed = innerTemplate;
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
                    // ループ内で置換済みのdata属性は削除
                    processed = processed
                        .replace(/\sdata-nyanString="[^"]*"/gi, "")
                        .replace(/\sdata-nyanClass="[^"]*"/gi, "")
                        .replace(/\sdata-nyanStyle="[^"]*"/gi, "")
                        .replace(/\sdata-nyanHref="[^"]*"/gi, "")
                        .replace(/\sdata-nyanId="[^"]*"/gi, "")
                        .replace(/\sdata-nyanChecked="[^"]*"/gi, "")
                        .replace(/\sdata-nyanSelected="[^"]*"/gi, "")
                        .replace(/\sdata-nyanDisabled="[^"]*"/gi, "")
                        .replace(/\sdata-nyanValue="[^"]*"/gi, "")
                        .replace(/\sdata-nyanName="[^"]*"/gi, "");
                    loopResult += processed;
                });
                return openTag + loopResult + closeTag;
            }
        );
    },

    // class指定
    setClass: function(htmlSegment, contextData) {
        return htmlSegment.replace(/data-nyanClass="(\w+)"/g, function(match, key) {
            return 'class="' + (contextData[key] !== undefined ? contextData[key] : "") + '"';
        });
    },

    // style指定
    setStyle: function(htmlSegment, contextData) {
        return htmlSegment.replace(/data-nyanStyle="(\w+)"/g, function(match, key) {
            return 'style="' + (contextData[key] !== undefined ? contextData[key] : "") + '"';
        });
    },

    // href指定
    setHref: function(htmlSegment, contextData) {
        return htmlSegment.replace(/data-nyanHref="(\w+)"/g, function(match, key) {
            return 'href="' + (contextData[key] !== undefined ? contextData[key] : "") + '"';
        });
    },

    // id指定
    setId: function(htmlSegment, contextData) {
        return htmlSegment.replace(/data-nyanId="(\w+)"/g, function(match, key) {
            return 'id="' + (contextData[key] !== undefined ? contextData[key] : "") + '"';
        });
    },

    // checked指定
    setChecked: function(htmlSegment, contextData) {
        return htmlSegment.replace(/data-nyanChecked="(\w+)"/g, function(match, key) {
            var val = contextData[key];
            if (val === true || val === 'checked') return 'checked';
            return '';
        });
    },

    // selected指定
    setSelected: function(htmlSegment, contextData) {
        return htmlSegment.replace(/data-nyanSelected="(\w+)"/g, function(match, key) {
            var val = contextData[key];
            if (val === true || val === 'selected') return 'selected';
            return '';
        });
    },

    // disabled指定
    setDisabled: function(htmlSegment, contextData) {
        return htmlSegment.replace(/data-nyanDisabled="(\w+)"/g, function(match, key) {
            var val = contextData[key];
            if (val === true || val === 'disabled') return 'disabled';
            return '';
        });
    },

    // value指定
    setValue: function(htmlSegment, contextData) {
        return htmlSegment.replace(/data-nyanValue="(\w+)"/g, function(match, key) {
            return 'value="' + (contextData[key] !== undefined ? contextData[key] : "") + '"';
        });
    },

    // name指定
    setName: function(htmlSegment, contextData) {
        return htmlSegment.replace(/data-nyanName="(\w+)"/g, function(match, key) {
            return 'name="' + (contextData[key] !== undefined ? contextData[key] : "") + '"';
        });
    }
};

// nyanPlate 関数
function nyanPlate(data, htmlCode) {
    htmlCode = htmlCode || nyanHtmlCode;

    // ループ処理
    htmlCode = nyanPlateScript.processLoop(htmlCode, data);

    // グローバル処理
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

    return htmlCode;
}
