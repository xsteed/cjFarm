export default {
  '*.{js,vue}': ['prettier --write', 'eslint --fix'],
  '*.{vue,css,less}': ['stylelint --fix'],
  '*.less': ['prettier --write']
};
