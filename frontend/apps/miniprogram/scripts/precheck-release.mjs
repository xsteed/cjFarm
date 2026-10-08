// ============================================================================
// 上线前预检：把「本地联调值」拦在提交之前
//
// 本地开发会把几处改成方便调试的值（API_ORIGIN 指本机、DEV_TABLE_CODE 填真实桌台码、
// urlCheck 关掉域名校验），它们**都能通过 tsc / eslint / 单测 / 冒烟** ——
// 只有在真机上才会暴露。所以这道检查不进 `npm run verify`（开发时本地值才是正确的），
// 提交或提测前单独跑一次。
//
// 用法：npm run mp:precheck   （有问题时以非 0 退出，可直接挂到 pre-commit）
// 详见 docs/miniprogram.md 的「开发期便利」。
// ============================================================================
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = path.dirname(fileURLToPath(import.meta.url));
const APP = path.dirname(ROOT);

const config = fs.readFileSync(path.join(APP, 'src/config.ts'), 'utf8');
const project = JSON.parse(fs.readFileSync(path.join(APP, 'project.config.json'), 'utf8'));

/** 从 config.ts 里抠出一个字符串常量的值（够用即可，不做完整 TS 解析）。 */
function pick(re) {
  const m = re.exec(config);
  return m ? m[1] : null;
}

const problems = [];

// ---- 1) API_ORIGIN 必须是 https 域名 ----
const origin = pick(/API_ORIGIN\s*=\s*'([^']+)'/);
if (!origin) {
  problems.push('config.ts 里没找到 API_ORIGIN');
} else if (!/^https:\/\//.test(origin)) {
  problems.push(`API_ORIGIN = ${origin} —— 必须是 https（小程序不接受 http）`);
} else if (/127\.0\.0\.1|localhost|^\d+(\.\d+){3}$/.test(origin.replace(/^https?:\/\//, ''))) {
  problems.push(`API_ORIGIN = ${origin} —— 本机地址 / 裸 IP，真机连不上`);
}

// ---- 2) DEV_TABLE_CODE 必须为空 ----
const devCode = pick(/DEV_TABLE_CODE\s*=\s*'([^']*)'/);
if (devCode) {
  problems.push(`DEV_TABLE_CODE = '${devCode}' —— 上线前置空，否则开发版会默认进这张桌`);
}

// ---- 3) urlCheck 必须开（别把本地的 false 带上生产）----
if (project?.setting?.urlCheck !== true) {
  problems.push(
    `project.config.json 的 setting.urlCheck = ${JSON.stringify(project?.setting?.urlCheck)} —— 应为 true`
  );
}

// ---- 4) appid 不能是游客号 ----
if (!project?.appid || project.appid === 'touristappid') {
  problems.push(`appid = ${project?.appid || '(空)'} —— 应填正式 AppID`);
}

// ---- 5) 私有配置里关掉的域名校验也要还原（该文件不入库，只在本机提醒）----
const privatePath = path.join(APP, 'project.private.config.json');
if (fs.existsSync(privatePath)) {
  const priv = JSON.parse(fs.readFileSync(privatePath, 'utf8'));
  // 私有文件不进 git，但会跟着小程序一起被工具读；上线前最好也勾回校验
  if (priv?.setting?.urlCheck === false) {
    problems.push(
      'project.private.config.json 的 setting.urlCheck = false —— 本机开发值，提测前在「详情 → 本地设置」勾回校验'
    );
  }
}

if (problems.length) {
  console.error('\n✗ 上线前预检未通过：\n');
  for (const p of problems) console.error(`  - ${p}`);
  console.error('\n修完再跑一次：npm run mp:precheck\n');
  process.exit(1);
}

console.log('\n✓ 上线前预检通过：config.ts 与 project.config.json 都是生产值\n');
console.log('  · 还要在公众平台配 request / downloadFile 合法域名（见 docs/miniprogram.md 第二节）');
console.log('  · 还要过一遍真机回归清单（见 docs/miniprogram.md 第五节末尾）\n');
