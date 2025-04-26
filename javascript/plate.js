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
            { title: "りんご", price: "100" , style: "color: red;", className: "apple" , list:[{title: "津軽"}, {title: "ふじ"}]},
            { title: "みかん", price: "50" , style: "color: white", className: "orange",  list:[{title: "温州みかん"}, {title: "デコポン"}]},
            { title: "バナナ", price: "80" , style: "color: orange", className: "banana", list:[]}
        ]};
    data.type = "種類A";
    console.log(data);
    let text_parts = nyanPlate(data, nyanGetFile("./html/parts_plate_table.html"));
    console.log(text_parts);
    data.data = text_parts;
    return nyanPlate(data);
}


main();