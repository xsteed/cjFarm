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
    'selector-pseudo-class-no-unknown': [
      true,
      {
        ignorePseudoClasses: ['deep', 'global', 'v-deep']
      }
    ]
  }
};
