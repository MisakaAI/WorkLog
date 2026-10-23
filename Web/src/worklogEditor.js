import StarterKit from '@tiptap/starter-kit'
import { generateJSON } from '@tiptap/vue-3'
import { Color, TextStyle } from '@tiptap/extension-text-style'
import { isWorklogColor } from './worklogColor.js'

// 富文本编辑与 HTML 导入共用扩展配置，支持范围需与后端文档校验规则保持一致。
export function createWorklogExtensions({ onUnsupportedColor = () => {} } = {}) {
  // 同时用于原生粘贴和源码导入，保留正文，只移除服务端不支持的颜色。
  const SupportedColor = Color.extend({
    addGlobalAttributes() {
      return this.parent().map(group => {
        const color = group.attributes.color
        return { ...group, attributes: { ...group.attributes, color: {
          ...color,
          parseHTML: element => {
            const value = color.parseHTML(element)
            if (!value || isWorklogColor(value)) return value
            onUnsupportedColor()
            return null
          },
        } } }
      })
    },
  })
  return [
    StarterKit.configure({ heading: { levels: [2, 3] }, link: false }),
    TextStyle,
    SupportedColor,
  ]
}

export function parseWorklogHTML(html, options) {
  const document = new DOMParser().parseFromString(html, 'text/html')
  // 源码只导入编辑器支持的文档节点，不执行脚本或嵌入页面。
  document.querySelectorAll('script, style, iframe, object, embed, meta, link, base, template').forEach(node => node.remove())
  return generateJSON(document.body.innerHTML, createWorklogExtensions(options))
}
