// qrcode 无官方类型声明（@types/qrcode 未安装），补一个最小模块声明，
// 让 apps/admin 的 tableQr.ts（以及顾客端用到的 qrcode）在 strict TS 下可被 vue-tsc 检查。
declare module 'qrcode';
