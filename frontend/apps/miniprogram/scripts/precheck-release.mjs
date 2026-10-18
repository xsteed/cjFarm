// ============================================================================
// 上线前预检：把「只在真机上才暴露」的配置问题拦在提审之前
//
// 这类问题**都能通过 tsc / eslint / 单测 / 冒烟**，只在真机或审核环节才炸：
//   ・生产域名还是占位符 / http / 本机地址  → 正式版全站请求失败
//   ・生产域名换了但没同步公众平台的合法域名白名单 → 请求被拒
//   ・隐私授权链路被删改 → getPhoneNumber 点了没反应、提审驳回
//
// 域名与开发便利值现在都按 envVersion 自动切换（见 src/config.ts），
// 所以这里检查的是**生产分支最终会用到什么**，而不是「当前源码里是不是本机值」——
// 后者在开发期本来就是本机值，检查它只会制造噪音。
//
// 不进 `npm run verify`：它检查的是「能不能上线」，与「代码对不对」是两件事。
// 提交或提测前单独跑一次。
//
// 用法：在 monorepo 根目录 frontend/ 下 `npm run mp:precheck`，
//       或在本包下 `npm run precheck`（两者是同一个脚本）。
//       有问题时以非 0 退出，可直接挂到 pre-commit。
// 详见 docs/miniprogram.md 的「开发期便利」与 docs/miniprogram-release-audit.md。
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

const appJson = JSON.parse(fs.readFileSync(path.join(APP, 'src/app.json'), 'utf8'));

const problems = [];

// ---- 1) PROD_API_ORIGIN 必须是真实可用的 https 域名 ----
//
// 注意检查对象是**生产分支**的常量，不是运行时的 API_ORIGIN。
// 后者现在由 envVersion 自动切换（开发版指向本机是合法的），
// 所以「当前值必须是 https」这条老规则会误报 —— 真正要保证的是
// 「正式版最终会打到哪个域名」。
const origin = pick(/PROD_API_ORIGIN\s*=\s*'([^']+)'/);
if (!origin) {
  problems.push('config.ts 里没找到 PROD_API_ORIGIN —— 生产域名必须显式声明，不能靠运行时拼接');
} else if (!/^https:\/\//.test(origin)) {
  problems.push(`PROD_API_ORIGIN = ${origin} —— 必须是 https（小程序不接受 http）`);
} else if (/example\.com|your-?domain|replace|todo|xxx/i.test(origin)) {
  problems.push(`PROD_API_ORIGIN = ${origin} —— 还是占位符，必须换成已 ICP 备案的真实域名`);
} else if (/127\.0\.0\.1|localhost|^\d+(\.\d+){3}$/.test(origin.replace(/^https?:\/\//, ''))) {
  problems.push(`PROD_API_ORIGIN = ${origin} —— 本机地址 / 裸 IP，真机连不上`);
}

// 防止有人把「按环境切换」改回硬编码单一域名：那样预检就再也检查不到生产值了
if (!/envVersion\s*\(/.test(config)) {
  problems.push('config.ts 缺少 envVersion() 环境切换 —— API_ORIGIN 不应硬编码为单一域名');
}

// ---- 2) 开发便利值不得泄漏到线上 ----
//
// DEV_TABLE_CODE 本身**不需要**清空：devTableCode() 有 envVersion 守卫，
// 体验版与正式版恒返回空串。这里只守住「守卫还在」这件事 ——
// 一旦有人删掉守卫，这个值就会变成正式版默认进某张桌的真实故障。
const devCode = pick(/DEV_TABLE_CODE\s*=\s*'([^']*)'/);
if (devCode && !/isDevelopEnv\s*\(|envVersion\s*\(/.test(config)) {
  problems.push(
    `DEV_TABLE_CODE = '${devCode}' 但没有环境守卫 —— 正式版会默认进这张桌，必须恢复 devTableCode 的 envVersion 判断`
  );
}

// ---- 2.5) 隐私接口必须有配套的授权链路 ----
//
// 这一项拦的是「只在提审 / 真机才暴露」的问题：tsc、eslint、单测、冒烟全都测不到 ——
// 本地开发环境里 getPhoneNumber 拿不到 code 只会表现为「点了没反应」。
//
// 代码侧的授权链路（pages/privacy + app.ts 的监听 + app.json 的开关）在这里查；
// 平台侧的《用户隐私保护指引》**代码无从自证**，只能靠提示语提醒（见末尾输出）。
{
  const pages = new Set(appJson.pages || []);
  if (appJson.__usePrivacyCheck__ !== true) {
    problems.push('src/app.json 缺 __usePrivacyCheck__: true —— 未开启隐私校验，提审会被驳回');
  }
  if (!pages.has('pages/privacy/index')) {
    problems.push('src/app.json 的 pages 里缺 pages/privacy/index —— 隐私接口的授权页不存在');
  }
  if (!pages.has('pages/agreement/index')) {
    problems.push('src/app.json 的 pages 里缺 pages/agreement/index —— 缺少协议/隐私政策的查阅入口');
  }
  const appTs = fs.readFileSync(path.join(APP, 'src/app.ts'), 'utf8');
  if (!/onNeedPrivacyAuthorization/.test(appTs)) {
    problems.push('src/app.ts 没有注册 wx.onNeedPrivacyAuthorization —— 隐私接口会被挂起且无法恢复');
  }
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

console.log('\n✓ 上线前预检通过：配置、隐私授权链路都是生产状态\n');
console.log('  · 还要在公众平台配 request / downloadFile 合法域名（见 docs/miniprogram.md 第二节）');
console.log('  · 还要过一遍真机回归清单（见 docs/miniprogram.md 第五节末尾）');
console.log('  · 以下事项代码侧无法自查，逐项确认后再提审（详见 docs/miniprogram-release-audit.md）：');
console.log('      1) 完成小程序备案（工信部），未备案不得开展互联网信息服务');
console.log('      2) 公众平台「设置 → 服务内容声明 → 用户隐私保护指引」已提交并审核通过');
console.log('      3) 类目与实际服务一致（餐饮需营业执照、食品经营许可证）');
console.log('      4) 主体已认证；若开放储值充值，确认预付资金的资质与类目许可');
console.log('      5) 新版需要通过「体验版 → 提交审核 → 发布」上线，审核通过前线上仍是旧版本\n');
