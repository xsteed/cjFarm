<template>
  <div class="page-card" style="max-width: 720px">
    <!-- 等配置回显并归一化完成后再挂载表单:开关组件的取值校验只在挂载/更新时触发,
         先挂载再回填 bad value 会直接抛错并中断整棵路由树渲染。 -->
    <div v-if="loading" class="cfg-loading">配置加载中…</div>
    <!-- 加载失败时不再渲染表单:避免把一张空白表单提交上去,把线上配置整表清空。 -->
    <div v-else-if="!loaded" class="cfg-loading">
      <p>配置加载失败，请检查后端服务后重试。</p>
      <t-button theme="primary" size="small" @click="load()">重新加载</t-button>
    </div>
    <t-form
      v-else
      :data="form"
      :label-width="isMobile ? '100%' : '130px'"
      :label-align="isMobile ? 'top' : 'right'"
    >
      <t-form-item label="店铺名称" name="shop_name">
        <t-input v-model="form.shop_name" placeholder="如 长健农场 柴火农家土菜" />
      </t-form-item>

      <t-form-item label="H5 访问地址" name="h5_base_url">
        <t-space direction="vertical" style="align-items: flex-start; width: 100%">
          <t-input
            v-model="form.h5_base_url"
            placeholder="如 http://1.2.3.4 或 https://dining.example.com（不带末尾斜杠）"
            style="width: 520px; max-width: 100%"
          />
          <span class="tip">
            桌台二维码会指向这个地址，必须是<b>手机能访问到</b>的公网域名或服务器 IP。
            填 localhost / 127.0.0.1 手机扫了会「访问不通」；留空则自动使用当前访问地址。
          </span>
        </t-space>
      </t-form-item>

      <t-form-item label="店铺 Logo" name="shop_logo">
        <div class="qr-item">
          <t-image v-if="form.shop_logo" :src="form.shop_logo" style="width: 120px; height: 120px; background: #fff" fit="contain" />
          <div v-else class="qr-empty">未上传</div>
          <div class="logo-side">
            <t-upload v-model="logoFiles" :auto-upload="false" accept="image/*" theme="image" :max="1" />
            <span class="tip">建议正方形透明底 PNG（≥ 300×300）。用于桌台二维码中心与打印桌牌。</span>
          </div>
        </div>
      </t-form-item>

      <t-form-item label="餐位费">
        <t-space direction="vertical" style="align-items: flex-start">
          <t-switch v-model="form.seat_fee_enabled" :custom-value="['1', '0']" />
          <t-input-number v-model="form.seat_fee" :min="0" :decimal-places="2" suffix="元/人" />
          <span class="tip">开启后下单按用餐人数自动计入订单</span>
        </t-space>
      </t-form-item>

      <t-form-item label="促销优惠">
        <t-space direction="vertical" style="align-items: flex-start">
          <t-switch v-model="form.promotion_enabled" :custom-value="['1', '0']" />
          <div class="promo-row">
            <span>满</span>
            <t-input-number v-model="form.promotion_threshold" :min="0" :decimal-places="2" />
            <span>元减</span>
            <t-input-number v-model="form.promotion_discount" :min="0" :decimal-places="2" />
            <span>元</span>
          </div>
        </t-space>
      </t-form-item>

      <t-form-item label="微信收款码" name="pay_qr_wx">
        <div class="qr-item">
          <t-image v-if="form.pay_qr_wx" :src="form.pay_qr_wx" style="width: 120px; height: 120px" fit="cover" />
          <div v-else class="qr-empty">未上传</div>
          <t-upload v-model="wxFiles" :auto-upload="false" accept="image/*" theme="image" :max="1" />
        </div>
      </t-form-item>

      <t-form-item label="支付宝收款码" name="pay_qr_ali">
        <div class="qr-item">
          <t-image v-if="form.pay_qr_ali" :src="form.pay_qr_ali" style="width: 120px; height: 120px" fit="cover" />
          <div v-else class="qr-empty">未上传</div>
          <t-upload v-model="aliFiles" :auto-upload="false" accept="image/*" theme="image" :max="1" />
        </div>
      </t-form-item>

      <t-divider>在线支付（未申请 key 前可保持关闭，不影响码牌收款）</t-divider>

      <t-form-item label="微信在线支付">
        <t-space direction="vertical" style="align-items: flex-start">
          <t-switch v-model="form.wxpay_enabled" :custom-value="['1', '0']" />
          <span class="tip">开启后顾客可微信在线支付（需填写下方商户参数）</span>
        </t-space>
      </t-form-item>
      <t-form-item label="微信商户号" name="wxpay_mchid">
        <t-input v-model="form.wxpay_mchid" placeholder="微信支付商户号 mchid" />
      </t-form-item>
      <t-form-item label="微信 AppID" name="wxpay_appid">
        <t-input v-model="form.wxpay_appid" placeholder="公众号/小程序 AppID" />
      </t-form-item>
      <t-form-item label="APIv3 密钥" name="wxpay_apiv3_key">
        <t-input v-model="form.wxpay_apiv3_key" type="password" placeholder="32位 APIv3 密钥（留空表示不修改）" />
      </t-form-item>
      <t-form-item label="证书序列号" name="wxpay_serial_no">
        <t-input v-model="form.wxpay_serial_no" placeholder="商户 API 证书序列号" />
      </t-form-item>
      <t-form-item label="商户私钥路径" name="wxpay_private_key_path">
        <t-input v-model="form.wxpay_private_key_path" placeholder="apiclient_key.pem 的绝对路径" />
      </t-form-item>
      <t-form-item label="平台证书路径" name="wxpay_platform_cert_path">
        <t-input v-model="form.wxpay_platform_cert_path" placeholder="平台证书 .pem 文件，或存放多张证书的目录" />
      </t-form-item>
      <t-form-item label="微信支付公钥ID" name="wxpay_pubkey_id">
        <t-input v-model="form.wxpay_pubkey_id" placeholder="商户平台「API安全」申请公钥后获得" />
      </t-form-item>
      <t-form-item label="微信支付公钥路径" name="wxpay_pubkey_path">
        <t-input v-model="form.wxpay_pubkey_path" placeholder="pub_key.pem 的绝对路径" />
      </t-form-item>
      <t-form-item>
        <span class="tip">
          验签方式二选一：填「公钥ID + 公钥路径」走官方推荐的公钥模式（无过期，无需换证）；仅填平台证书路径则走证书模式（5 年需换证，可填目录以支持新旧证书并存）
        </span>
      </t-form-item>
      <t-form-item label="微信回调地址" name="wxpay_notify_url">
        <t-input v-model="form.wxpay_notify_url" placeholder="https://域名/prod-api/api/dining/pay/notify/wxpay" />
      </t-form-item>

      <t-form-item label="支付宝在线支付">
        <t-space direction="vertical" style="align-items: flex-start">
          <t-switch v-model="form.alipay_enabled" :custom-value="['1', '0']" />
          <span class="tip">开启后顾客可支付宝在线支付</span>
        </t-space>
      </t-form-item>
      <t-form-item label="支付宝 AppID" name="alipay_appid">
        <t-input v-model="form.alipay_appid" placeholder="支付宝开放平台应用 AppID" />
      </t-form-item>
      <t-form-item label="应用私钥路径" name="alipay_private_key_path">
        <t-input v-model="form.alipay_private_key_path" placeholder="应用私钥 .pem 的绝对路径" />
      </t-form-item>
      <t-form-item label="支付宝公钥" name="alipay_public_key">
        <t-input v-model="form.alipay_public_key" placeholder="支付宝公钥（PEM 或纯 base64）" />
      </t-form-item>
      <t-form-item label="支付宝回调地址" name="alipay_notify_url">
        <t-input v-model="form.alipay_notify_url" placeholder="https://域名/prod-api/api/dining/pay/notify/alipay" />
      </t-form-item>

      <t-divider>小票打印</t-divider>

      <t-form-item label="打印总开关">
        <t-space direction="vertical" style="align-items: flex-start">
          <t-switch v-model="form.print_enabled" :custom-value="['1', '0']" />
          <span class="tip">
            关闭后下单、加菜、结账都不再自动出纸（打印机管理里的「补打」仍可用）。
            临时缺纸或调试时可先关掉，避免打印机队列堆积。
          </span>
        </t-space>
      </t-form-item>
      <t-form-item label="厨房单显示金额">
        <t-space direction="vertical" style="align-items: flex-start">
          <t-switch v-model="form.print_kitchen_show_price" :custom-value="['1', '0']" />
          <span class="tip">开启后厨房单会带出每道菜的小计，便于后厨或传菜核对；默认关闭，后厨只看菜名与数量。</span>
        </t-space>
      </t-form-item>
      <t-form-item label="飞鹅账号" name="feie_user">
        <t-input v-model="form.feie_user" placeholder="飞鹅云后台注册账号（手机号/邮箱）" />
      </t-form-item>
      <t-form-item label="飞鹅 UKEY" name="feie_ukey">
        <t-input v-model="form.feie_ukey" type="password" placeholder="开发者 UKEY（留空表示不修改）" />
      </t-form-item>
      <t-form-item label="飞鹅接口地址" name="feie_api_url">
        <t-input v-model="form.feie_api_url" placeholder="默认 https://api.de.feieyun.com/Api/Open/" />
      </t-form-item>
      <t-form-item label=" ">
        <span class="tip">
          UKEY 在飞鹅云后台「个人中心」获取，<b>不是</b>打印机机身上的识别码 KEY；填错会报签名校验失败(-3)。
          账号配置好后，到「打印机管理」把打印机通道选成「飞鹅云」并填写 SN 即可跨网络出纸。
        </span>
      </t-form-item>

      <t-divider>本地打印代理（后端部署在云服务器、门店已有 9100 网络机时用）</t-divider>

      <t-form-item label="代理令牌" name="agent_token">
        <t-space>
          <t-input
            v-model="form.agent_token"
            type="password"
            style="width: 320px"
            placeholder="留空表示不修改；未配置时「本地代理」通道的打印机不会出纸"
          />
          <t-button variant="outline" @click="genAgentToken">生成随机令牌</t-button>
        </t-space>
      </t-form-item>
      <t-form-item label=" ">
        <span class="tip">
          把它填到门店内网那台常开机设备的 print-agent 上（<code>--token</code> 或 agent.env）。
          代理是<b>出站</b>连云端的：门店不需要公网 IP、不需要端口映射、不需要 VPN，
          也不需要安装任何打印机驱动（9100 是 RAW 端口，打印机直接收字节流）。
          <br />
          改动令牌后，所有已部署的代理都要同步更新，否则会一直报「代理令牌不正确」。
          完整步骤见 <code>docs/print-agent.md</code>。
        </span>
      </t-form-item>

      <t-form-item>
        <t-button theme="primary" :loading="saving" :disabled="!loaded || !canEdit" @click="save">保存配置</t-button>
        <span v-if="!canEdit" class="tip" style="margin-left: 10px">当前账号只能查看配置，不能修改</span>
      </t-form-item>
    </t-form>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import { getConfig, saveConfig, uploadFile } from '../api'
import { useIsMobile } from '../utils/useMobile'
import { hasPerm } from '../utils/perm'

const { isMobile } = useIsMobile()

// config:view 可看，config:edit 才能改。只读账号禁用保存按钮(后端 saveConfig 同样 403)。
const canEdit = computed(() => hasPerm('config:edit'))

const form = reactive({
  shop_name: '', shop_logo: '', h5_base_url: '', seat_fee_enabled: '0', seat_fee: '0',
  promotion_enabled: '0', promotion_threshold: '0', promotion_discount: '0',
  pay_qr_wx: '', pay_qr_ali: '',
  wxpay_enabled: '0', alipay_enabled: '0',
  wxpay_mchid: '', wxpay_appid: '', wxpay_apiv3_key: '', wxpay_serial_no: '',
  wxpay_private_key_path: '', wxpay_platform_cert_path: '',
  wxpay_pubkey_id: '', wxpay_pubkey_path: '', wxpay_notify_url: '',
  alipay_appid: '', alipay_private_key_path: '', alipay_public_key: '', alipay_notify_url: '',
  print_enabled: '1', print_kitchen_show_price: '0',
  feie_user: '', feie_ukey: '', feie_api_url: 'https://api.de.feieyun.com/Api/Open/',
  // 敏感项：后端不回显，留空表示不修改（与飞鹅 UKEY 同一套语义）
  agent_token: ''
})
const logoFiles = ref([])
const wxFiles = ref([])
const aliFiles = ref([])
const saving = ref(false)
const loading = ref(true)
// loaded 表示「已成功回显过一次配置」。只有 loaded 为真才允许保存,
// 否则一次误提交就会把线上配置整表覆盖成默认空值(历史事故的成因之一)。
const loaded = ref(false)

// 开关型字段:t-switch 的 custom-value 只认 '1' / '0'。
// 若绑定值出现空串或意外取值,组件会在更新阶段抛
// `value is not in ["1","0"]`,该异常会打断整棵路由树的 patch ——
// 现象就是「打开过系统配置页后,再点其他菜单全部白屏」。
// 因此回显前一律归一化,不要相信后端/历史数据的取值。
const FLAG_KEYS = [
  'seat_fee_enabled', 'promotion_enabled', 'wxpay_enabled', 'alipay_enabled',
  'print_enabled', 'print_kitchen_show_price'
]

function normFlag(v) {
  return String(v ?? '') === '1' ? '1' : '0'
}

// silent = true 表示保存后的静默回显:不切换 loading 态(避免表单卸载重挂的闪烁),
// 失败时也保留当前已加载状态。
async function load(silent = false) {
  if (!silent) {
    loading.value = true
    loaded.value = false
  }
  try {
    const res = await getConfig()
    Object.assign(form, res)
    form.shop_logo = res.shop_logo || ''
    // 数值字段转为数字,便于 t-input-number 绑定
    form.seat_fee = Number(res.seat_fee) || 0
    form.promotion_threshold = Number(res.promotion_threshold) || 0
    form.promotion_discount = Number(res.promotion_discount) || 0
    // 开关字段归一化为 '1' / '0'
    FLAG_KEYS.forEach((k) => {
      form[k] = normFlag(res[k])
    })
    loaded.value = true
  } catch (e) {
    MessagePlugin.error(e?.message || '配置加载失败，请稍后重试')
  } finally {
    if (!silent) loading.value = false
  }
}

async function save() {
  if (!canEdit.value) {
    MessagePlugin.warning('当前账号只能查看配置，不能修改')
    return
  }
  if (!loaded.value) {
    MessagePlugin.warning('配置尚未加载成功，请先重新加载')
    return
  }
  saving.value = true
  try {
    if (wxFiles.value.length) {
      const f = wxFiles.value[0].raw || wxFiles.value[0]
      if (f) form.pay_qr_wx = (await uploadFile(f)).url
    }
    if (aliFiles.value.length) {
      const f = aliFiles.value[0].raw || aliFiles.value[0]
      if (f) form.pay_qr_ali = (await uploadFile(f)).url
    }
    // 数值字段转回字符串,与后端约定一致;开关字段兜底为 '1'/'0'
    const payload = { ...form }
    FLAG_KEYS.forEach((k) => {
      payload[k] = normFlag(form[k])
    })
    payload.seat_fee = String(form.seat_fee)
    payload.promotion_threshold = String(form.promotion_threshold)
    payload.promotion_discount = String(form.promotion_discount)
    await saveConfig(payload)
    MessagePlugin.success('保存成功')
    // 保存后重新拉取,保证界面与库里的真实值一致
    await load()
    // 代理令牌是敏感项、后端不回显:保存后清空输入框,避免它一直明文挂在页面上,
    // 也避免下次保存时把同一个值再提交一遍(虽然后端是幂等的,但看着容易误解)。
    form.agent_token = ''
  } catch (e) {
    MessagePlugin.error(e?.message || '保存失败，请稍后重试')
  } finally {
    saving.value = false
  }
}

// 生成随机代理令牌。
//
// 代理令牌等同于「一台打印机的操作权限」:拿到它就能把待打印队列整个拉走
// (含订单金额),所以不能手填成 123456 这种。字符集剔除 0/O/1/I/l 等易混字符,
// 便于门店在另一台设备上照着敲。
function genAgentToken() {
  const chars = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789'
  const buf = new Uint8Array(32)
  crypto.getRandomValues(buf)
  form.agent_token = Array.from(buf, (b) => chars[b % chars.length]).join('')
  MessagePlugin.info('已生成随机令牌：保存配置后，把它填到门店那台设备的 print-agent 上')
}

onMounted(load)
</script>

<style scoped>
.tip {
  color: #999;
  font-size: 12px;
}
.qr-item {
  display: flex;
  gap: 16px;
  align-items: center;
}
/* 促销「满 X 元减 Y 元」:窄屏折行后数字输入框要保持可点的宽度,
   否则会被 flex 压到只剩十几像素 */
.promo-row {
  display: flex;
  gap: 8px;
  align-items: center;
  flex-wrap: wrap;
}
.promo-row .t-input-number {
  flex: 0 0 auto;
  min-width: 92px;
}
.logo-side {
  display: flex;
  flex-direction: column;
  gap: 6px;
  max-width: 320px;
}
.qr-empty {
  width: 120px;
  height: 120px;
  background: #f0f0f0;
  color: #bbb;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 6px;
}
.cfg-loading {
  padding: 60px 0;
  text-align: center;
  color: #999;
  font-size: 13px;
}

/* 移动端:Logo / 收款码的「图片 + 上传框」并排会挤爆,改为上下堆叠 */
@media (max-width: 767px) {
  .qr-item {
    flex-direction: column;
    align-items: flex-start;
    gap: 10px;
  }
  .logo-side {
    max-width: 100%;
  }
  .t-form__label--top {
    padding-right: 0;
  }
}
</style>
