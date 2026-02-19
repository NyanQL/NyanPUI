const data = nyanReadFileB64("./html/images/nyan.png");

({
  status: 200,
  headers: { "Cache-Control": "no-store" },
  contentType: "image/png",
  body: {
    encoding: "base64",
    data: data
  }
});
