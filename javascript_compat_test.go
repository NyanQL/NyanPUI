package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
)

// Exercise the shipped libraries through the same include and parameter path as
// endpoint scripts. These cases cover regexp replacement, backreferences and
// string concatenation across JavaScript engine upgrades.
func runPlateCompatibilityScript(t *testing.T, template, script string, data map[string]interface{}) string {
	t.Helper()
	oldInclude := globalConfig.JavaScriptInclude
	t.Cleanup(func() { globalConfig.JavaScriptInclude = oldInclude })
	globalConfig.JavaScriptInclude = nil
	for _, name := range []string{"nyanPlate.js", "nyanPlateToJson.js"} {
		path, err := filepath.Abs(filepath.Join("javascript", "lib", name))
		if err != nil {
			t.Fatal(err)
		}
		globalConfig.JavaScriptInclude = append(globalConfig.JavaScriptInclude, path)
	}
	value, err := runJavaScriptValueWithContext(nil, nil,
		writeFixtureFile(t, script), writeFixtureFile(t, template),
		map[string]interface{}{"data": data})
	if err != nil {
		t.Fatal(err)
	}
	return value.String()
}

func TestNyanPlateRenderingWithGoja(t *testing.T) {
	for _, tc := range []struct {
		name     string
		template string
		data     map[string]interface{}
		want     string
	}{
		{
			name:     "unicode_and_replacement_characters",
			template: `<h1 data-nyanString="title">old</h1><p data-nyanString="title"></p><b data-nyanString="missing"><!--既定 🐾--></b>`,
			data:     map[string]interface{}{"title": "にゃん😺 $& $1"},
			want:     `<h1 data-nyanDoneNyanString="title">にゃん😺 $& $1</h1><p data-nyanDoneNyanString="title">にゃん😺 $& $1</p><b data-nyanDoneNyanString="missing">既定 🐾</b>`,
		},
		{
			name:     "attributes_and_booleans",
			template: `<input data-nyanId="id" data-nyanClass="class" data-nyanValue="value" data-nyanChecked="checked" data-nyanDisabled="disabled"/><a data-nyanHref="href" data-nyanString="label"></a>`,
			data: map[string]interface{}{
				"id": "cat", "class": "active", "value": "日本語😺", "checked": true,
				"disabled": false, "href": "/cats?q=1&lang=ja", "label": "詳細",
			},
			want: `<input id="cat" data-nyanDoneId="id" class="active" data-nyanDoneClass="class" value="日本語😺" data-nyanDoneValue="value" checked data-nyanDoneChecked="checked" data-nyanDoneDisabled="disabled"/><a href="/cats?q=1&lang=ja" data-nyanDoneHref="href" data-nyanDoneNyanString="label">詳細</a>`,
		},
		{
			name:     "nested_loops_keep_item_values",
			template: `<section data-nyanLoop="groups"><article><h2 data-nyanString="name"></h2><ul data-nyanLoop="items"><li data-nyanString="name"></li></ul></article></section>`,
			data: map[string]interface{}{
				"name": "親の値",
				"groups": []interface{}{
					map[string]interface{}{"name": "猫", "items": []interface{}{
						map[string]interface{}{"name": "三毛😺"}, map[string]interface{}{"name": "黒猫🐈‍⬛"},
					}},
					map[string]interface{}{"name": "犬", "items": []interface{}{
						map[string]interface{}{"name": "柴犬🐕"},
					}},
				},
			},
			want: `<section data-nyanLoop="groups"><article><h2 data-nyanDoneNyanString="name">猫</h2><ul data-nyanLoop="items"><li data-nyanDoneNyanString="name">三毛😺</li><li data-nyanDoneNyanString="name">黒猫🐈‍⬛</li></ul></article><article><h2 data-nyanDoneNyanString="name">犬</h2><ul data-nyanLoop="items"><li data-nyanDoneNyanString="name">柴犬🐕</li></ul></article></section>`,
		},
		{
			name:     "empty_loop",
			template: `<ul data-nyanLoop="items"><li data-nyanString="name">default</li></ul>`,
			data:     map[string]interface{}{"items": []interface{}{}},
			want:     `<ul data-nyanLoop="items"></ul>`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := runPlateCompatibilityScript(t, tc.template, `nyanPlate(nyanAllParams.data);`, tc.data)
			if got != tc.want {
				t.Fatalf("rendered HTML = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNyanPlateJSONRoundTripWithGoja(t *testing.T) {
	items := make([]interface{}, 128)
	for i := range items {
		items[i] = map[string]interface{}{
			"name": fmt.Sprintf("猫 %d 😺 $&", i),
			"href": fmt.Sprintf("/cats/%d?lang=ja&view=詳細", i),
		}
	}
	data := map[string]interface{}{"title": "一覧🐾", "items": items}
	const template = `<h1 data-nyanString="title"></h1><ul data-nyanLoop="items"><li><a data-nyanHref="href" data-nyanString="name"></a></li></ul>`
	gotJSON := runPlateCompatibilityScript(t, template,
		`JSON.stringify(nyanPlateToJson(nyanPlate(nyanAllParams.data)));`, data)
	var got map[string]interface{}
	if err := json.Unmarshal([]byte(gotJSON), &got); err != nil {
		t.Fatalf("invalid JSON: %v; output=%q", err, gotJSON)
	}
	if !reflect.DeepEqual(got, data) {
		t.Fatalf("round trip = %#v, want %#v", got, data)
	}
}
