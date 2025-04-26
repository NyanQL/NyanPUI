console.log("loaded plateDecode.js");

function main() {
    const html = nyanGetAPI("http://localhost:8009/plate" , "", "");
    const data = nyanPlateToJson(html);
    console.log(JSON.stringify(data, null, 2));
    return nyanPlate({"json": JSON.stringify(data, null, 2)});
}

main();