/*
にゃんぷれーと
文字列の置き換え data-nyanString="key"
ループ処理 data-nyanLoop ="key"
htmlタグへのclassの指定 data-nyanClass = "key"
htmlタグへのstyleの指定 data-nyanStyle = "key"
リンクの指定 data-nyanHref = "key"
処理の実行は nyanPlate(data, htmlCode);
各処理は nyanPlateScript のオブジェクトにして他のライブラリとの混在を避ける。
*/
console.log("loaded nyanPlate.js.");

// nyanPlateScript オブジェクトに各処理を実装
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
                    // ループ内で置換済みのdata属性は削除
                    processed = processed
                        .replace(/\sdata-nyanString="[^"]*"/gi, "")
                        .replace(/\sdata-nyanClass="[^"]*"/gi, "")
                        .replace(/\sdata-nyanStyle="[^"]*"/gi, "")
                        .replace(/\sdata-nyanHref="[^"]*"/gi, "");
                    loopResult += processed;
                });
                return openTag + loopResult + closeTag;
            }
        );
    },

    // HTMLタグへのclass指定
    setClass: function(htmlSegment, contextData) {
        return htmlSegment.replace(/data-nyanClass="(\w+)"/g, function(match, key) {
            return 'class="' + (contextData[key] !== undefined ? contextData[key] : "") + '"';
        });
    },

    // HTMLタグへのstyle指定
    setStyle: function(htmlSegment, contextData) {
        return htmlSegment.replace(/data-nyanStyle="(\w+)"/g, function(match, key) {
            return 'style="' + (contextData[key] !== undefined ? contextData[key] : "") + '"';
        });
    },

    // HTMLタグへのhref指定: data-nyanHref="key" を href="値" に置き換え
    setHref: function(htmlSegment, contextData) {
        return htmlSegment.replace(/data-nyanHref="(\w+)"/g, function(match, key) {
            return 'href="' + (contextData[key] !== undefined ? contextData[key] : "") + '"';
        });
    }
};

// nyanPlate 関数: 全体のHTMLに対して各置換処理を実行します
function nyanPlate(data, htmlCode) {
    htmlCode = htmlCode || nyanHtmlCode;

    // まずループ処理を実行
    htmlCode = nyanPlateScript.processLoop(htmlCode, data);

    // グローバル置換処理
    htmlCode = nyanPlateScript.setString(htmlCode, data);
    htmlCode = nyanPlateScript.setClass(htmlCode, data);
    htmlCode = nyanPlateScript.setStyle(htmlCode, data);
    htmlCode = nyanPlateScript.setHref(htmlCode, data);

    return htmlCode;
}
