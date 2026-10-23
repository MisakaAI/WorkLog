package domain

import (
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"
)

var hexColor = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)
var colorFunction = regexp.MustCompile(`^(rgb|rgba|hsl|hsla)\(([^()]*)\)$`)
var cssNumber = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:e[+-]?[0-9]+)?$`)
var namedColors = func() map[string]bool {
	out := map[string]bool{}
	for _, s := range strings.Fields(`aliceblue antiquewhite aqua aquamarine azure beige bisque black blanchedalmond blue blueviolet brown burlywood cadetblue chartreuse chocolate coral cornflowerblue cornsilk crimson cyan darkblue darkcyan darkgoldenrod darkgray darkgrey darkgreen darkkhaki darkmagenta darkolivegreen darkorange darkorchid darkred darksalmon darkseagreen darkslateblue darkslategray darkslategrey darkturquoise darkviolet deeppink deepskyblue dimgray dimgrey dodgerblue firebrick floralwhite forestgreen fuchsia gainsboro ghostwhite gold goldenrod gray grey green greenyellow honeydew hotpink indianred indigo ivory khaki lavender lavenderblush lawngreen lemonchiffon lightblue lightcoral lightcyan lightgoldenrodyellow lightgray lightgrey lightgreen lightpink lightsalmon lightseagreen lightskyblue lightslategray lightslategrey lightsteelblue lightyellow lime limegreen linen magenta maroon mediumaquamarine mediumblue mediumorchid mediumpurple mediumseagreen mediumslateblue mediumspringgreen mediumturquoise mediumvioletred midnightblue mintcream mistyrose moccasin navajowhite navy oldlace olive olivedrab orange orangered orchid palegoldenrod palegreen paleturquoise palevioletred papayawhip peachpuff peru pink plum powderblue purple rebeccapurple red rosybrown royalblue saddlebrown salmon sandybrown seagreen seashell sienna silver skyblue slateblue slategray slategrey snow springgreen steelblue tan teal thistle tomato turquoise violet wheat white whitesmoke yellow yellowgreen transparent currentcolor`) {
		out[s] = true
	}
	return out
}()
var language = regexp.MustCompile(`^[a-zA-Z0-9_+#./\-]{0,80}$`)

// safeColor 仅接受明确支持的 CSS 颜色语法与数值范围，不接受任意 CSS 表达式。
func safeColor(s string) bool {
	if len(s) > 128 {
		return false
	}
	if hexColor.MatchString(s) {
		return true
	}
	s = strings.ToLower(s)
	if namedColors[s] {
		return true
	}
	m := colorFunction.FindStringSubmatch(s)
	if m == nil {
		return false
	}
	body := m[2]
	var parts []string
	comma := strings.Contains(body, ",")
	if comma {
		parts = strings.Split(body, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		if len(parts) != 3 && len(parts) != 4 {
			return false
		}
	} else {
		parts = strings.Fields(strings.ReplaceAll(body, "/", " / "))
		if len(parts) == 5 && parts[3] == "/" {
			parts = append(parts[:3], parts[4])
		} else if len(parts) != 3 {
			return false
		}
	}
	number := func(s string, min, max float64) bool {
		if !cssNumber.MatchString(s) {
			return false
		}
		v, e := strconv.ParseFloat(s, 64)
		return e == nil && !math.IsInf(v, 0) && v >= min && v <= max
	}
	percent := func(s string) bool { return strings.HasSuffix(s, "%") && number(strings.TrimSuffix(s, "%"), 0, 100) }
	if strings.HasPrefix(m[1], "rgb") {
		percentCount := 0
		for _, p := range parts[:3] {
			if strings.HasSuffix(p, "%") {
				if !percent(p) {
					return false
				}
				percentCount++
			} else if !number(p, 0, 255) {
				return false
			}
		}
		if comma && percentCount != 0 && percentCount != 3 {
			return false
		}
	} else {
		hue := parts[0]
		for _, unit := range []string{"deg", "grad", "turn", "rad"} {
			if strings.HasSuffix(hue, unit) {
				hue = strings.TrimSuffix(hue, unit)
				break
			}
		}
		if !number(hue, -360000, 360000) || !percent(parts[1]) || !percent(parts[2]) {
			return false
		}
	}
	if len(parts) == 4 && !percent(parts[3]) && !number(parts[3], 0, 1) {
		return false
	}
	return true
}

type node struct {
	Type    string                     `json:"type"`
	Attrs   map[string]json.RawMessage `json:"attrs,omitempty"`
	Content []json.RawMessage          `json:"content,omitempty"`
	Marks   []json.RawMessage          `json:"marks,omitempty"`
	Text    *string                    `json:"text,omitempty"`
}
type mark struct {
	Type  string                     `json:"type"`
	Attrs map[string]json.RawMessage `json:"attrs,omitempty"`
}

func attrs(a map[string]json.RawMessage, allowed ...string) bool {
	for k := range a {
		if !member(k, allowed...) {
			return false
		}
	}
	return true
}
func integer(b json.RawMessage, min, max int64) bool {
	var n int64
	return len(b) > 0 && strings.TrimSpace(string(b)) != "null" && json.Unmarshal(b, &n) == nil && n >= min && n <= max
}
func nullableString(b json.RawMessage, check func(string) bool) bool {
	if string(b) == "null" {
		return true
	}
	var s string
	return json.Unmarshal(b, &s) == nil && check(s)
}
func validateMarks(ms []json.RawMessage) error {
	seen := map[string]bool{}
	for _, raw := range ms {
		var m mark
		if StrictJSON(raw, &m, "type") != nil || seen[m.Type] {
			return Bad("文档 mark 非法或重复")
		}
		seen[m.Type] = true
		switch m.Type {
		case "bold", "italic", "underline", "strike", "code":
			if len(m.Attrs) != 0 {
				return Bad("格式属性非法")
			}
		case "textStyle":
			if !attrs(m.Attrs, "color") {
				return Bad("字体属性非法")
			}
			if c, ok := m.Attrs["color"]; ok && !nullableString(c, safeColor) {
				return Bad("字体颜色非法")
			}
		default:
			return Bad("不支持的文档格式")
		}
	}
	if seen["code"] && len(ms) > 1 {
		return Bad("行内代码不能与其他 mark 混用")
	}
	return nil
}

// ValidateDocument 按 ProseMirror 的节点嵌套规则校验文档，并返回是否含有效内容。
// 原始 JSON 保持不变，包括 Tiptap 自动生成的 null 默认属性。
func ValidateDocument(raw json.RawMessage) (bool, error) {
	if len(raw) > 1<<20 {
		return false, &Error{413, "PAYLOAD_TOO_LARGE", "日志不能超过 1 MiB"}
	}
	count := 0
	var walk func(json.RawMessage, string, int) (string, bool, error)
	walk = func(raw json.RawMessage, parent string, depth int) (string, bool, error) {
		// 同时限制深度和节点总数，避免异常文档耗尽递归栈或处理时间。
		count++
		if depth > 64 || count > 50000 {
			return "", false, Bad("文档嵌套过深或节点过多")
		}
		var n node
		if err := StrictJSON(raw, &n, "type"); err != nil {
			return "", false, err
		}
		fail := func() (string, bool, error) { return "", false, Bad("文档节点结构或属性非法：" + n.Type) }
		if err := validateMarks(n.Marks); err != nil {
			return "", false, err
		}
		if len(n.Marks) > 0 && !member(n.Type, "text", "hardBreak") {
			return fail()
		}
		if parent == "codeBlock" && len(n.Marks) > 0 {
			return fail()
		}
		if n.Text != nil && n.Type != "text" {
			return fail()
		}
		switch n.Type {
		case "doc":
			if parent != "" || len(n.Attrs) > 0 || len(n.Content) == 0 {
				return fail()
			}
		case "paragraph", "blockquote", "bulletList", "listItem":
			if len(n.Attrs) > 0 {
				return fail()
			}
		case "heading":
			if !attrs(n.Attrs, "level") || !integer(n.Attrs["level"], 2, 3) {
				return fail()
			}
		case "orderedList":
			if !attrs(n.Attrs, "start", "type") {
				return fail()
			}
			if v, ok := n.Attrs["start"]; ok && !integer(v, -MaxVersion, MaxVersion) {
				return fail()
			}
			if v, ok := n.Attrs["type"]; ok && !nullableString(v, func(s string) bool { return member(s, "1", "a", "A", "i", "I") }) {
				return fail()
			}
		case "codeBlock":
			if !attrs(n.Attrs, "language") {
				return fail()
			}
			if v, ok := n.Attrs["language"]; ok && !nullableString(v, func(s string) bool { return language.MatchString(s) }) {
				return fail()
			}
		case "text":
			if n.Text == nil || *n.Text == "" || len(n.Content) > 0 || len(n.Attrs) > 0 {
				return fail()
			}
		case "horizontalRule", "hardBreak":
			if len(n.Content) > 0 || len(n.Attrs) > 0 {
				return fail()
			}
		default:
			return fail()
		}
		if parent == "" && n.Type != "doc" {
			return fail()
		}
		switch parent {
		case "paragraph", "heading":
			if !member(n.Type, "text", "hardBreak") {
				return fail()
			}
		case "codeBlock":
			if n.Type != "text" {
				return fail()
			}
		case "bulletList", "orderedList":
			if n.Type != "listItem" {
				return fail()
			}
		case "doc", "blockquote", "listItem":
			if !member(n.Type, "paragraph", "heading", "bulletList", "orderedList", "blockquote", "codeBlock", "horizontalRule") {
				return fail()
			}
		}
		if member(n.Type, "bulletList", "orderedList", "blockquote", "listItem") && len(n.Content) == 0 {
			return fail()
		}
		has := n.Type == "horizontalRule" || n.Text != nil && Trim(*n.Text) != ""
		for i, c := range n.Content {
			typ, h, err := walk(c, n.Type, depth+1)
			if err != nil {
				return "", false, err
			}
			if n.Type == "listItem" && i == 0 && typ != "paragraph" {
				return fail()
			}
			has = has || h
		}
		return n.Type, has, nil
	}
	_, has, err := walk(raw, "", 0)
	return has, err
}
