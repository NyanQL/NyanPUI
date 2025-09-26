// ./js/markdown.js

const editor = document.getElementById("editor");
const preview = document.getElementById("preview");

// Markdown → HTML に変換して表示
function updatePreview() {
    preview.innerHTML = markdownToHtml(editor.value);
}

// 初期表示
updatePreview();
editor.addEventListener("input", updatePreview);

// ================================
// 画像ファイルのドラッグ＆ドロップ対応
// ================================

// エディタにドラッグしたときの見た目変更
editor.addEventListener("dragover", (e) => {
    e.preventDefault();
    editor.style.border = "2px dashed #666";
});

editor.addEventListener("dragleave", () => {
    editor.style.border = "";
});

editor.addEventListener("drop", (e) => {
    e.preventDefault();
    editor.style.border = "";

    const files = e.dataTransfer.files;
    if (!files || files.length === 0) return;

    [...files].forEach((file) => {
        if (!file.type.startsWith("image/")) {
            alert("画像ファイルのみ対応しています。");
            return;
        }

        const reader = new FileReader();
        reader.onload = function (ev) {
            const base64 = ev.target.result; // data:image/png;base64,xxxx
            const insertText = `\n\n![${file.name}](${base64})\n\n`;

            // カーソル位置に挿入
            const start = editor.selectionStart;
            const end = editor.selectionEnd;
            const text = editor.value;
            editor.value = text.substring(0, start) + insertText + text.substring(end);

            // カーソルを挿入したテキストの末尾に移動
            editor.selectionStart = editor.selectionEnd = start + insertText.length;

            // プレビュー更新
            updatePreview();
        };
        reader.readAsDataURL(file);
    });
});
