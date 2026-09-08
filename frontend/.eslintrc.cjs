// 最小可用的 ESLint flat config（项目缺配置文件，pre-commit hook 跑 lint 时报错）
// 严格度跟 tsconfig strict 持平
module.exports = {
  root: true,
  env: { browser: true, es2020: true },
  extends: [
    'eslint:recommended',
    'plugin:@typescript-eslint/recommended',
    'plugin:react-hooks/recommended',
  ],
  ignorePatterns: ['dist', 'node_modules', '.eslintrc.cjs'],
  parser: '@typescript-eslint/parser',
  parserOptions: {
    ecmaVersion: 'latest',
    sourceType: 'module',
    ecmaFeatures: { jsx: true },
  },
  plugins: ['react-refresh', '@typescript-eslint'],
  rules: {
    'react-refresh/only-export-components': 'off', // 项目里用 React 函数组件直 export 是 OK 的
    '@typescript-eslint/no-unused-vars': ['warn', { argsIgnorePattern: '^_', varsIgnorePattern: '^_' }],
    '@typescript-eslint/no-explicit-any': 'off',
    'no-empty-pattern': 'off',
    'no-useless-escape': 'off',
    // 回归守卫（docs/FIX-PLAN-FRONTEND-TOKEN.md §3.3）：防误写，不是安全边界。
    // 挡的是「类别」而非单条字符串：手拼鉴权头 / 死掉的 localStorage token 源 / 不存在的 /v1 前缀。
    // 如需豁免，改这里并走 code review。
    'no-restricted-syntax': ['error',
      {
        // 不限定对象：裸 `localStorage`、`window.localStorage`、`sessionStorage` 等价写法都要挡
        // （对象约束只匹配裸标识量，`window.localStorage` 的 callee.object 是 MemberExpression）。
        selector: "CallExpression[callee.property.name='getItem'] > Literal[value='token']",
        message: '鉴权已改为 httpOnly cookie（C-F5），storage 里没有 token；请用 services/api 的共享 client。',
      },
      {
        // 大小写都挡（HTTP 头名不区分大小写）：Identifier 键 / 字面量键两种写法。
        selector: "Identifier[name=/^authorization$/i], Literal[value=/^authorization$/i]",
        message: '禁止手拼 Authorization 头（后端见到该头就不回退 cookie，空 token 恒 401）；请用 services/api 的共享 client。',
      },
      {
        selector: "Literal[value=/^\\/api\\/v1(\\/|$)/i]",
        message: '后端没有 /v1 前缀（routes.go 只挂 /api）；请用 /api/... 路径。',
      },
      {
        selector: "TemplateElement[value.raw=/^\\/api\\/v1(\\/|$)/i]",
        message: '后端没有 /v1 前缀（routes.go 只挂 /api）；请用 /api/... 路径。',
      },
    ],
  },
}
