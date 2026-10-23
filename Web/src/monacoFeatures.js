// 必须先注册核心编辑器：它会注册 HTML worker、格式化等依赖的编辑器服务。
import 'monaco-editor/features/codeEditor/register.js'
import 'monaco-editor/features/codicon/register.js'
import 'monaco-editor/features/tokenization/register.js'

// HTML 源码编辑需要的常用操作；不注册 Diff、CodeLens、内联补全等高级功能。
import 'monaco-editor/features/bracketMatching/register.js'
import 'monaco-editor/features/clipboard/register.js'
import 'monaco-editor/features/comment/register.js'
import 'monaco-editor/features/contextmenu/register.js'
import 'monaco-editor/features/find/register.js'
import 'monaco-editor/features/folding/register.js'
import 'monaco-editor/features/format/register.js'
import 'monaco-editor/features/gotoLine/register.js'
import 'monaco-editor/features/hover/register.js'
import 'monaco-editor/features/indentation/register.js'
import 'monaco-editor/features/linesOperations/register.js'
import 'monaco-editor/features/snippet/register.js'
import 'monaco-editor/features/toggleTabFocusMode/register.js'
import 'monaco-editor/features/wordOperations/register.js'

// Monaco 0.57 的 features/suggest 只注册内联补全提供器；弹出式补全需单独注册控制器。
import 'monaco-editor/editor/contrib/suggest/browser/suggestController.js'
