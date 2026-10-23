// 与 internal/domain/document.go 的颜色协议保持一致；共享契约样本覆盖两端。
const namedColors = new Set(`aliceblue antiquewhite aqua aquamarine azure beige bisque black blanchedalmond blue blueviolet brown burlywood cadetblue chartreuse chocolate coral cornflowerblue cornsilk crimson cyan darkblue darkcyan darkgoldenrod darkgray darkgrey darkgreen darkkhaki darkmagenta darkolivegreen darkorange darkorchid darkred darksalmon darkseagreen darkslateblue darkslategray darkslategrey darkturquoise darkviolet deeppink deepskyblue dimgray dimgrey dodgerblue firebrick floralwhite forestgreen fuchsia gainsboro ghostwhite gold goldenrod gray grey green greenyellow honeydew hotpink indianred indigo ivory khaki lavender lavenderblush lawngreen lemonchiffon lightblue lightcoral lightcyan lightgoldenrodyellow lightgray lightgrey lightgreen lightpink lightsalmon lightseagreen lightskyblue lightslategray lightslategrey lightsteelblue lightyellow lime limegreen linen magenta maroon mediumaquamarine mediumblue mediumorchid mediumpurple mediumseagreen mediumslateblue mediumspringgreen mediumturquoise mediumvioletred midnightblue mintcream mistyrose moccasin navajowhite navy oldlace olive olivedrab orange orangered orchid palegoldenrod palegreen paleturquoise palevioletred papayawhip peachpuff peru pink plum powderblue purple rebeccapurple red rosybrown royalblue saddlebrown salmon sandybrown seagreen seashell sienna silver skyblue slateblue slategray slategrey snow springgreen steelblue tan teal thistle tomato turquoise violet wheat white whitesmoke yellow yellowgreen transparent currentcolor`.split(' '))
const cssNumber = /^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:e[+-]?[0-9]+)?$/
const whitespace = /[\t-\r \u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+/
const trim = value => value.split(whitespace).filter(Boolean).join(' ')
const number = (value, min, max) => cssNumber.test(value) && Number.isFinite(Number(value)) && Number(value) >= min && Number(value) <= max
const percent = value => value.endsWith('%') && number(value.slice(0, -1), 0, 100)

export function isWorklogColor(value) {
  if (typeof value !== 'string' || new TextEncoder().encode(value).length > 128) return false
  if (/^#(?:[\da-f]{3}|[\da-f]{4}|[\da-f]{6}|[\da-f]{8})$/i.test(value)) return true
  const color = value.toLowerCase()
  if (namedColors.has(color)) return true
  const match = /^(rgb|rgba|hsl|hsla)\(([^()]*)\)$/.exec(color)
  if (!match) return false
  const comma = match[2].includes(',')
  let parts = comma ? match[2].split(',').map(trim) : trim(match[2].replaceAll('/', ' / ')).split(' ')
  if (comma) {
    if (parts.length !== 3 && parts.length !== 4) return false
  } else if (parts.length === 5 && parts[3] === '/') {
    parts = [...parts.slice(0, 3), parts[4]]
  } else if (parts.length !== 3) return false
  if (match[1].startsWith('rgb')) {
    if (!parts.slice(0, 3).every(part => part.endsWith('%') ? percent(part) : number(part, 0, 255))) return false
    const percentCount = parts.slice(0, 3).filter(part => part.endsWith('%')).length
    if (comma && percentCount !== 0 && percentCount !== 3) return false
  } else {
    const hue = parts[0].replace(/(?:deg|grad|turn|rad)$/, '')
    if (!number(hue, -360000, 360000) || !percent(parts[1]) || !percent(parts[2])) return false
  }
  return parts.length !== 4 || percent(parts[3]) || number(parts[3], 0, 1)
}
