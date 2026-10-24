export default {
  extends: ['stylelint-config-standard'],
  plugins: ['stylelint-less'],
  overrides: [
    {
      files: ['**/*.vue'],
      customSyntax: 'postcss-html'
    },
    {
      files: ['**/*.less'],
      customSyntax: 'postcss-less'
    }
  ],
  rules: {
    'custom-property-pattern': ['^(?:[a-z][a-z0-9]*(?:-[a-z0-9]+)*|td-brand-color-\\d+)$'],
    'media-query-no-invalid': null,
    'declaration-property-value-no-unknown': null,
    'selector-class-pattern': ['^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$', { severity: 'warning' }],
    'no-descending-specificity': [true, { severity: 'warning' }],
    'declaration-property-value-keyword-no-deprecated': [true, { severity: 'warning' }],
    // 以下厂商前缀是有意保留的兼容写法,降为 warning 而不是删掉:
    //   -moz-/-webkit-appearance: 隐藏 number 输入框的步进箭头(Firefox 与 WebKit
    //   各认各自的前缀,删了步进箭头会回来);
    //   -webkit-user-select: 旧版 iOS Safari 只认带前缀的 user-select。
    'property-no-vendor-prefix': [true, { severity: 'warning' }],
    'selector-pseudo-class-no-unknown': [
      true,
      {
        ignorePseudoClasses: ['deep', 'global', 'v-deep']
      }
    ]
  }
};
