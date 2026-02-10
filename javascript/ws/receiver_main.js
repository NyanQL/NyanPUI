// ws_client の受信サンプル
// - nyanAllParams.ws_message_type: "text" / "binary" / ...
// - nyanAllParams.ws_message_text: テキスト（binary でも string(data) が入る）
// - nyanAllParams.ws_message_json: テキストが JSON として decode できた場合のみ入る
// - 返り値が空文字以外なら、その内容を WebSocket へ TextMessage で返信します

var payload = nyanAllParams.ws_message_json;
if (payload === undefined) {
  payload = nyanAllParams.ws_message_text;
}
console.log("[ws_client:" + nyanAllParams.ws_client + "] type=" + nyanAllParams.ws_message_type, payload);

""; // 返信しない
