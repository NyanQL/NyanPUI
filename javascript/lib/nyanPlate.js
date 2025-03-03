/*
にゃんぷれーと
文字列の置き換え data-nyanString="key"
ループ処理 data-nyanLoop ="key"
htmlタグへのclassの指定 data-nyanClass = "key"
htmlタグへのstypeの指定 data-nyanStyle = "key"
処理の実行は nyanPlate(data, htmlCode); //htmlCodeは省略可能 省略した場合にはnyanHtmlCodeを使用する。
各処理は nyanPlateScript のオブジェクトにして他のライブラリとの混在を避ける。
nyanPlateScript.setString(htmlSegment, contextData); //文字列の置き換え
nyanPlateScript.processLoop(htmlSegment, contextData); //ループ処理
nyanPlateScript.setClass(htmlSegment, contextData); //classの指定
nyanPlateScript.setStyle(htmlSegment, contextData); //styleの指定
*/
console.log("loaded nyanPlate.js.");

// nyanPlateScript オブジェクトに各処理を実装
const nyanPlateScript = {
    // 文字列の置換: data-nyanString="key" のタグ内テキストを contextData[key] に置き換えます
    setString: function(htmlSegment, contextData) {
        return htmlSegment.replace(
            /(<\w+[^>]*data-nyanString="(\w+)"[^>]*>)(?:(<!--\s*([^<]+?)\s*-->)|([^<]*))(<\/\w+>)/g,
            function(match, openTag, key, commentWrapper, commentContent, plainText, closeTag) {
                // contextData[key] があればその値を、なければ元のコメント/テキストを使います
                var replacement = contextData[key] !== undefined ? contextData[key] : (commentContent || plainText);
                return openTag + replacement + closeTag;
            }
        );
    },

    // ループ処理: data-nyanLoop="key" を持つ<div>タグ内のテンプレート部分を、
    // contextData[key] の配列の各要素に対して処理します
    processLoop: function(htmlSegment, contextData) {
        return htmlSegment.replace(/(<(\w+)[^>]*data-nyanLoop="(\w+)"[^>]*>)([\s\S]*?)(<\/\2>)/gi, function(match, openTag, tagName, loopKey, innerTemplate, closeTag) {
            var items = contextData[loopKey];
            if (!items || !Array.isArray(items)) return match;
            var loopResult = "";
            items.forEach(function(item) {
                var processed = innerTemplate;
                processed = nyanPlateScript.setString(processed, item);
                processed = nyanPlateScript.setClass(processed, item);
                processed = nyanPlateScript.setStyle(processed, item);
                // ループ内で置換済みのdata属性は削除しておく
                processed = processed.replace(/\sdata-nyanString="[^"]*"/gi, "")
                    .replace(/\sdata-nyanClass="[^"]*"/gi, "")
                    .replace(/\sdata-nyanStyle="[^"]*"/gi, "");
                loopResult += processed;
            });
            return openTag + loopResult + closeTag;
        });
    },


    // HTMLタグへのclass指定: data-nyanClass="key" を、contextData[key] の値で class 属性に置き換えます
    setClass: function(htmlSegment, contextData) {
        return htmlSegment.replace(/data-nyanClass="(\w+)"/g, function(match, key) {
            return 'class="' + (contextData[key] !== undefined ? contextData[key] : "") + '"';
        });
    },

    // HTMLタグへのstyle指定: data-nyanStyle="key" を、contextData[key] の値で style 属性に置き換えます
    setStyle: function(htmlSegment, contextData) {
        return htmlSegment.replace(/data-nyanStyle="(\w+)"/g, function(match, key) {
            return 'style="' + (contextData[key] !== undefined ? contextData[key] : "") + '"';
        });
    }
};

// nyanPlate 関数: 全体のHTMLに対して各置換処理を実行します
function nyanPlate(data, htmlCode) {
    // htmlCode が省略された場合、デフォルトの nyanHtmlCode を利用
    htmlCode = htmlCode || nyanHtmlCode;

    // まずループ処理を実行して、ループ部だけは item ごとの置換で展開する
    htmlCode = nyanPlateScript.processLoop(htmlCode, data);

    // 次に、残りのグローバルな置換処理を実施
    htmlCode = nyanPlateScript.setString(htmlCode, data);
    htmlCode = nyanPlateScript.setClass(htmlCode, data);
    htmlCode = nyanPlateScript.setStyle(htmlCode, data);

    return htmlCode;
}
