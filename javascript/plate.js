/*
* nyanPlateを動作確認するためのサンプルコードです。
* */
console.log("loaded plate.js");

function main() {
    let data = {
        "title": "タイトル",
        "sub_title": "サブタイトル",
        "title_style": "text-align: center; color: blue;",
        "className": "orange",
        items: [
            { title: "りんご", price: "100" , style: "color: red;", className: "apple"},
            { title: "みかん", price: "50" , style: "color: white", className: "orange"},
            { title: "バナナ", price: "80" , style: "color: orange", className: "banana"}
        ]
    };
    return nyanPlate(data);
}


main();