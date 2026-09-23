<template>
  <div
    class="page-card"
    style="max-width: 720px"
  >
    <!-- 等配置回显并归一化完成后再挂载表单:开关组件的取值校验只在挂载/更新时触发,
         先挂载再回填 bad value 会直接抛错并中断整棵路由树渲染。 -->
    <div
      v-if="loading"
      class="cfg-loading"
    >
      配置加载中…
    </div>
    <!-- 加载失败时不再渲染表单:避免把一张空白表单提交上去,把线上配置整表清空。 -->
    <div
      v-else-if="!loaded"
      class="cfg-loading"
    >
      <p>配置加载失败，请检查后端服务后重试。</p>
      <t-button
        theme="primary"
        size="small"
        @click="load()"
      >
        重新加载
      </t-button>
    </div>
    <t-form
      v-else
      :data="form"
      :label-width="isMobile ? '100%' : '130px'"
      :label-align="isMobile ? 'top' : 'right'"
    >
      <t-tabs
        v-model="activeTab"
        class="cfg-tabs"
      >
        <t-tab-panel
          value="basic"
          label="基础设置"
        >
          <t-form-item
            label="店铺名称"
            name="shop_name"
          >
            <t-input
              v-model="form.shop_name"
              placeholder="如 长健农场 柴火农家土菜"
            />
          </t-form-item>

          <t-form-item
            label="H5 访问地址"
            name="h5_base_url"
          >
            <t-space
              direction="vertical"
              style="align-items: flex-start; width: 100%"
            >
              <t-input
                v-model="form.h5_base_url"
                placeholder="如 http://1.2.3.4 或 https://dining.example.com（不带末尾斜杠）"
                style="width: 520px; max-width: 100%"
              />
              <span class="tip">
                桌台二维码会指向这个地址，必须是<b>手机能访问到</b>的公网域名或服务器 IP。 填 localhost / 127.0.0.1
                手机扫了会「访问不通」；留空则自动使用当前访问地址。
              </span>
            </t-space>
          </t-form-item>

          <t-form-item
            label="店铺 Logo"
            name="shop_logo"
          >
            <div class="qr-item">
              <t-image
                v-if="form.shop_logo"
                :src="form.shop_logo"
                style="width: 120px; height: 120px; background: #fff"
                fit="contain"
              />
              <div
                v-else
                class="qr-empty"
              >
                未上传
              </div>
              <div class="logo-side">
                <t-upload
                  v-model="logoFiles"
                  :auto-upload="false"
                  accept="image/*"
                  theme="image"
                  :max="1"
                />
                <span class="tip">建议正方形透明底 PNG（≥ 300×300）。用于桌台二维码中心与打印桌牌。</span>
              </div>
            </div>
          </t-form-item>

          <t-form-item label="餐位费">
            <t-space
              direction="vertical"
              style="align-items: flex-start"
            >
              <t-switch
                v-model="form.seat_fee_enabled"
                :custom-value="['1', '0']"
              />
              <t-input-number
                v-model="form.seat_fee"
                :min="0"
                :decimal-places="2"
                suffix="元/人"
              />
              <span class="tip">开启后下单按用餐人数自动计入订单</span>
            </t-space>
          </t-form-item>

          <t-form-item label="促销优惠">
            <t-space
              direction="vertical"
              style="align-items: flex-start"
            >
              <t-switch
                v-model="form.promotion_enabled"
                :custom-value="['1', '0']"
              />
              <div class="promo-row">
                <span>满</span>
                <t-input-number
                  v-model="form.promotion_threshold"
                  :min="0"
                  :decimal-places="2"
                />
                <span>元减</span>
                <t-input-number
                  v-model="form.promotion_discount"
                  :min="0"
                  :decimal-places="2"
                />
                <span>元</span>
              </div>
            </t-space>
          </t-form-item>

          <t-form-item
            label="微信收款码"
            name="pay_qr_wx"
          >
            <div class="qr-item">
              <t-image
                v-if="form.pay_qr_wx"
                :src="form.pay_qr_wx"
                style="width: 120px; height: 120px"
                fit="cover"
              />
              <div
                v-else
                class="qr-empty"
              >
                未上传
              </div>
              <t-upload
                v-model="wxFiles"
                :auto-upload="false"
                accept="image/*"
                theme="image"
                :max="1"
              />
            </div>
          </t-form-item>

          <t-form-item
            label="支付宝收款码"
            name="pay_qr_ali"
          >
            <div class="qr-item">
              <t-image
                v-if="form.pay_qr_ali"
                :src="form.pay_qr_ali"
                style="width: 120px; height: 120px"
                fit="cover"
              />
              <div
                v-else
                class="qr-empty"
              >
                未上传
              </div>
              <t-upload
                v-model="aliFiles"
                :auto-upload="false"
                accept="image/*"
                theme="image"
                :max="1"
              />
            </div>
          </t-form-item>
        </t-tab-panel>

        <t-tab-panel
          value="pay"
          label="在线支付"
        >
          <div class="tab-note">未申请支付 key 前可保持关闭，不影响扫码牌收款。</div>
          <div class="cfg-subhead">微信支付</div>
          <t-form-item label="微信在线支付">
            <t-space
              direction="vertical"
              style="align-items: flex-start"
            >
              <t-switch
                v-model="form.wxpay_enabled"
                :custom-value="['1', '0']"
              />
              <span class="tip">开启后顾客可微信在线支付（需填写下方商户参数）</span>
            </t-space>
          </t-form-item>
          <t-form-item
            label="微信商户号"
            name="wxpay_mchid"
          >
            <t-input
              v-model="form.wxpay_mchid"
              placeholder="微信支付商户号 mchid"
            />
          </t-form-item>
          <t-form-item
            label="微信 AppID"
            name="wxpay_appid"
          >
            <t-input
              v-model="form.wxpay_appid"
              placeholder="公众号/小程序 AppID"
            />
          </t-form-item>
          <t-form-item
            label="APIv3 密钥"
            name="wxpay_apiv3_key"
          >
            <t-input
              v-model="form.wxpay_apiv3_key"
              type="password"
              placeholder="32位 APIv3 密钥（留空表示不修改）"
            />
          </t-form-item>
          <t-form-item
            label="证书序列号"
            name="wxpay_serial_no"
          >
            <t-input
              v-model="form.wxpay_serial_no"
              placeholder="商户 API 证书序列号"
            />
          </t-form-item>
          <t-form-item
            label="商户私钥路径"
            name="wxpay_private_key_path"
          >
            <t-input
              v-model="form.wxpay_private_key_path"
              placeholder="apiclient_key.pem 的绝对路径"
            />
          </t-form-item>
          <t-form-item
            label="平台证书路径"
            name="wxpay_platform_cert_path"
          >
            <t-input
              v-model="form.wxpay_platform_cert_path"
              placeholder="平台证书 .pem 文件，或存放多张证书的目录"
            />
          </t-form-item>
          <t-form-item
            label="微信支付公钥ID"
            name="wxpay_pubkey_id"
          >
            <t-input
              v-model="form.wxpay_pubkey_id"
              placeholder="商户平台「API安全」申请公钥后获得"
            />
          </t-form-item>
          <t-form-item
            label="微信支付公钥路径"
            name="wxpay_pubkey_path"
          >
            <t-input
              v-model="form.wxpay_pubkey_path"
              placeholder="pub_key.pem 的绝对路径"
            />
          </t-form-item>
          <t-form-item>
            <span class="tip">
              验签方式二选一：填「公钥ID +
              公钥路径」走官方推荐的公钥模式（无过期，无需换证）；仅填平台证书路径则走证书模式（5
              年需换证，可填目录以支持新旧证书并存）
            </span>
          </t-form-item>
          <t-form-item
            label="微信回调地址"
            name="wxpay_notify_url"
          >
            <t-input
              v-model="form.wxpay_notify_url"
              placeholder="https://域名/api/customer/pay/notify/wxpay"
            />
          </t-form-item>

          <div class="cfg-subhead">支付宝支付</div>
          <t-form-item label="支付宝在线支付">
            <t-space
              direction="vertical"
              style="align-items: flex-start"
            >
              <t-switch
                v-model="form.alipay_enabled"
                :custom-value="['1', '0']"
              />
              <span class="tip">开启后顾客可支付宝在线支付</span>
            </t-space>
          </t-form-item>
          <t-form-item
            label="支付宝 AppID"
            name="alipay_appid"
          >
            <t-input
              v-model="form.alipay_appid"
              placeholder="支付宝开放平台应用 AppID"
            />
          </t-form-item>
          <t-form-item
            label="应用私钥路径"
            name="alipay_private_key_path"
          >
            <t-input
              v-model="form.alipay_private_key_path"
              placeholder="应用私钥 .pem 的绝对路径"
            />
          </t-form-item>
          <t-form-item
            label="支付宝公钥"
            name="alipay_public_key"
          >
            <t-input
              v-model="form.alipay_public_key"
              placeholder="支付宝公钥（PEM 或纯 base64）"
            />
          </t-form-item>
          <t-form-item
            label="支付宝回调地址"
            name="alipay_notify_url"
          >
            <t-input
              v-model="form.alipay_notify_url"
              placeholder="https://域名/api/customer/pay/notify/alipay"
            />
          </t-form-item>
        </t-tab-panel>

        <t-tab-panel
          value="print"
          label="小票打印"
        >
          <t-form-item label="打印总开关">
            <t-space
              direction="vertical"
              style="align-items: flex-start"
            >
              <t-switch
                v-model="form.print_enabled"
                :custom-value="['1', '0']"
              />
              <span class="tip">
                关闭后下单、加菜、结账都不再自动出纸（打印机管理里的「补打」仍可用）。
                临时缺纸或调试时可先关掉，避免打印机队列堆积。
              </span>
            </t-space>
          </t-form-item>
          <t-form-item label="厨房单显示金额">
            <t-space
              direction="vertical"
              style="align-items: flex-start"
            >
              <t-switch
                v-model="form.print_kitchen_show_price"
                :custom-value="['1', '0']"
              />
              <span class="tip"
                >开启后厨房单会带出每道菜的小计，便于后厨或传菜核对；默认关闭，后厨只看菜名与数量。</span
              >
            </t-space>
          </t-form-item>
          <t-form-item
            label="小票页脚文案"
            name="print_guest_footer"
          >
            <t-space
              direction="vertical"
              style="align-items: flex-start"
            >
              <t-input
                v-model="form.print_guest_footer"
                placeholder="默认：谢谢惠顾,欢迎再次光临；清空则不打印页脚"
                style="width: 320px"
              />
              <span class="tip">
                打印在食客小票末尾的文案，可改成店铺口号。保存后到「打印记录 → 预览」即可看到效果，无需真实出纸。
              </span>
            </t-space>
          </t-form-item>
          <t-form-item label="小票显示餐位费">
            <t-space
              direction="vertical"
              style="align-items: flex-start"
            >
              <t-switch
                v-model="form.print_guest_show_seat_fee"
                :custom-value="['1', '0']"
              />
              <span class="tip">关闭后食客小票不再单列餐位费行（合计金额不变，仅影响展示）。</span>
            </t-space>
          </t-form-item>
          <t-form-item label="小票显示优惠">
            <t-space
              direction="vertical"
              style="align-items: flex-start"
            >
              <t-switch
                v-model="form.print_guest_show_discount"
                :custom-value="['1', '0']"
              />
              <span class="tip">关闭后食客小票不再单列优惠行；有优惠金额时才出现该行。</span>
            </t-space>
          </t-form-item>
          <t-form-item label="模板预览">
            <t-space
              direction="vertical"
              style="align-items: flex-start"
            >
              <t-space>
                <t-radio-group
                  v-model="sampleDocType"
                  variant="default-filled"
                  size="small"
                >
                  <t-radio-button value="guest">食客小票</t-radio-button>
                  <t-radio-button value="kitchen">厨房单</t-radio-button>
                </t-radio-group>
                <t-radio-group
                  v-model="samplePaperWidth"
                  variant="default-filled"
                  size="small"
                >
                  <t-radio-button :value="48">80mm</t-radio-button>
                  <t-radio-button :value="32">58mm</t-radio-button>
                </t-radio-group>
                <t-button
                  theme="primary"
                  variant="outline"
                  size="small"
                  :loading="sampleLoading"
                  @click="loadSamplePreview"
                >
                  预览模板
                </t-button>
              </t-space>
              <span class="tip">
                用示例数据按<b>当前表单里的值</b>即时渲染（未保存也能看）；改完点按钮刷新。正式票据在「打印记录 →
                预览」查看。
              </span>
            </t-space>
          </t-form-item>
          <t-form-item
            label="飞鹅账号"
            name="feie_user"
          >
            <t-input
              v-model="form.feie_user"
              placeholder="飞鹅云后台注册账号（手机号/邮箱）"
            />
          </t-form-item>
          <t-form-item
            label="飞鹅 UKEY"
            name="feie_ukey"
          >
            <t-input
              v-model="form.feie_ukey"
              type="password"
              placeholder="开发者 UKEY（留空表示不修改）"
            />
          </t-form-item>
          <t-form-item
            label="飞鹅接口地址"
            name="feie_api_url"
          >
            <t-input
              v-model="form.feie_api_url"
              placeholder="默认 https://api.de.feieyun.com/Api/Open/"
            />
          </t-form-item>
          <t-form-item label=" ">
            <span class="tip">
              UKEY 在飞鹅云后台「个人中心」获取，<b>不是</b>打印机机身上的识别码 KEY；填错会报签名校验失败(-3)。
              账号配置好后，到「打印机管理」把打印机通道选成「飞鹅云」并填写 SN 即可跨网络出纸。
            </span>
          </t-form-item>
        </t-tab-panel>

        <t-tab-panel
          value="agent"
          label="打印代理"
        >
          <div class="tab-note">
            后端部署在云服务器、门店已有 9100 网络打印机时使用：门店设备装 print-agent 出站连云端取单，无需公网
            IP、端口映射或 VPN。
          </div>
          <!-- 代理运行状态:无论用全局令牌还是逐台签发,打开 tab 即见在线/离线与积压 -->
          <div class="agent-card agent-status">
            <div class="agent-card-hd">
              <span class="agent-name">代理运行状态</span>
              <t-tag
                v-if="agentStatus.configured"
                :theme="agentStatus.online ? 'success' : 'warning'"
                variant="light"
              >
                {{ agentStatus.online ? '在线' : '离线' }}
              </t-tag>
              <t-tag
                v-else
                theme="default"
                variant="light"
              >
                未配置
              </t-tag>
            </div>
            <div
              v-if="agentStatus.configured"
              class="agent-grid"
            >
              <div class="agent-cell">
                <div class="k">最近心跳</div>
                <div class="v">{{ agentStatus.lastSeen || '尚未收到过心跳' }}</div>
              </div>
              <div class="agent-cell">
                <div class="k">队列积压</div>
                <div class="v">
                  {{ agentStatus.pending }} 单{{ agentStatus.dead ? '（' + agentStatus.dead + ' 单已放弃）' : '' }}
                </div>
              </div>
              <div
                v-if="agentStatus.oldestPendingSec > 0"
                class="agent-cell"
              >
                <div class="k">最老待打印</div>
                <div class="v">已等 {{ agentWaitMinutes }} 分钟</div>
              </div>
            </div>
            <div
              v-else
              class="tip"
            >
              尚未配置代理令牌：门店内网的 print-agent 无法连接云端。请点下方「签发新令牌」（兼容旧代理的全局令牌），
              或在下面「打印代理身份管理」里逐台签发独立身份。
            </div>
          </div>
          <t-form-item
            label="代理令牌"
            name="agent_token"
          >
            <t-space>
              <t-input
                v-model="form.agent_token"
                type="password"
                style="width: 320px"
                placeholder="留空表示不修改；未配置时「本地代理」通道的打印机不会出纸"
              />
              <t-button
                v-if="canEdit"
                variant="outline"
                :loading="issuingToken"
                @click="genAgentToken"
              >
                签发新令牌
              </t-button>
              <!-- 敏感项留空=不修改,「清空」只能靠这个一次性标记随保存提交 -->
              <t-button
                v-if="canEdit"
                variant="outline"
                theme="danger"
                @click="onClearAgentToken"
              >
                清空令牌
              </t-button>
            </t-space>
            <div
              v-if="agentTokenClear"
              class="field-tip"
            >
              已标记清空：点击页面底部「保存配置」后，全局代理令牌被置空，所有用它的旧代理立即失联。
            </div>
          </t-form-item>
          <!-- label 必须保持短(130px 列宽 + nowrap),超长部分会被输入框盖住 -->
          <t-form-item
            label="代理最新版本号"
            name="agent_latest_version"
          >
            <t-space
              direction="vertical"
              style="align-items: flex-start"
            >
              <t-input
                v-model="form.agent_latest_version"
                style="width: 320px"
                placeholder="如 1.2.0"
              />
              <span class="tip"> 填写后随代理心跳下发给门店，代理发现版本落后会提示门店升级；留空则不提示。 </span>
            </t-space>
          </t-form-item>
          <t-form-item label=" ">
            <span class="tip">
              <b>「签发新令牌」由后端生成并立即落库生效，不必再点页面底部的保存；明文只显示一次，请当场复制。</b>
              把它填到门店内网那台常开机设备的 print-agent 上（<code>--token</code> 或 agent.env）。
              代理是<b>出站</b>连云端的：门店不需要公网 IP、不需要端口映射、不需要 VPN，
              也不需要安装任何打印机驱动（9100 是 RAW 端口，打印机直接收字节流）。
              <br />
              改动令牌后，所有已部署的代理都要同步更新，否则会一直报「代理令牌不正确」。 完整步骤见
              <code>docs/print-agent.md</code>。
            </span>
          </t-form-item>
          <t-form-item label=" ">
            <span class="tip">此为兼容旧版代理的全局令牌，新代理请逐台签发独立令牌。</span>
          </t-form-item>

          <t-divider>打印代理身份管理</t-divider>

          <t-form-item label=" ">
            <div class="agent-mgr">
              <div class="agent-toolbar">
                <span class="agent-title">已签发代理身份</span>
                <t-button
                  v-if="canEditAgents"
                  theme="primary"
                  variant="outline"
                  size="small"
                  @click="openAgentAdd"
                >
                  新增代理
                </t-button>
              </div>
              <div
                v-if="!agents.length"
                class="agent-empty"
              >
                暂无打印代理身份
              </div>
              <div
                v-for="a in agents"
                :key="a.agentId"
                class="agent-card"
              >
                <div class="agent-card-hd">
                  <span class="agent-name">{{ a.agentName }}</span>
                  <t-tag
                    :theme="a.status === 1 ? 'success' : 'default'"
                    variant="light"
                  >
                    {{ a.status === 1 ? '启用' : '已吊销' }}
                  </t-tag>
                  <!-- 在线判定与后端 agentOnlineWindow(90s)一致:最近心跳距今 < 90 秒 -->
                  <t-tag
                    v-if="a.status === 1"
                    :theme="isAgentOnline(a) ? 'success' : 'warning'"
                    variant="light"
                  >
                    {{ isAgentOnline(a) ? '在线' : '离线' }}
                  </t-tag>
                </div>
                <div class="agent-grid">
                  <div class="agent-cell">
                    <div class="k">令牌前缀</div>
                    <div class="v">
                      <code>{{ a.tokenHint || '—' }}</code>
                    </div>
                  </div>
                  <div class="agent-cell">
                    <div class="k">授权范围</div>
                    <div class="v">{{ printerScopeText(a) }}</div>
                  </div>
                  <div class="agent-cell">
                    <div class="k">最近心跳</div>
                    <div class="v">{{ a.lastSeen || '从未' }}</div>
                  </div>
                  <div class="agent-cell">
                    <div class="k">上报信息</div>
                    <div class="v">{{ a.lastReport || '—' }}</div>
                  </div>
                </div>
                <div class="agent-card-ft">
                  <t-button
                    v-if="canEditAgents"
                    variant="text"
                    size="small"
                    @click="openAgentEdit(a)"
                  >
                    编辑
                  </t-button>
                  <t-button
                    v-if="canEditAgents"
                    :theme="a.status === 1 ? 'warning' : 'success'"
                    variant="text"
                    size="small"
                    @click="onToggleAgentStatus(a)"
                  >
                    {{ a.status === 1 ? '吊销' : '恢复' }}
                  </t-button>
                  <!-- 只清理已吊销的代理:启用中的要先吊销,避免把正在出纸的代理抹掉 -->
                  <t-button
                    v-if="canEditAgents && a.status !== 1"
                    theme="danger"
                    variant="text"
                    size="small"
                    @click="onDeleteAgent(a)"
                  >
                    删除
                  </t-button>
                  <span
                    v-if="canEditAgents && a.status === 1"
                    class="tip"
                    >启用中不可删除，需先吊销</span
                  >
                </div>
              </div>
            </div>
          </t-form-item>
        </t-tab-panel>
      </t-tabs>

      <div class="cfg-footer">
        <t-button
          theme="primary"
          :loading="saving"
          :disabled="!loaded || !canEdit"
          @click="save"
        >
          保存配置
        </t-button>
        <span
          v-if="!canEdit"
          class="tip"
          >当前账号只能查看配置，不能修改</span
        >
      </div>
    </t-form>

    <!-- 票据模板示例预览:按当前表单值渲染,所见即所改 -->
    <TicketPreviewDialog
      v-model:visible="sampleVisible"
      :title="sampleTitle"
      :loading="sampleLoading"
      :chunks="sampleData?.chunks"
      :line-width="sampleData?.lineWidth ?? 48"
    >
      <template #meta>
        示例数据 + <b>当前表单值</b>渲染(未保存也能看);纸宽
        {{ sampleData?.lineWidth === 32 ? '58mm' : '80mm' }}。保存后新订单按此模板出纸。
      </template>
    </TicketPreviewDialog>

    <!-- 新增 / 编辑打印代理身份(编辑只改名称与授权范围,不重新签发令牌) -->
    <t-dialog
      v-model:visible="agentDialogVisible"
      :header="agentDialogMode === 'edit' ? '编辑打印代理' : '新增打印代理'"
      width="520px"
      :confirm-btn="{
        content: agentDialogMode === 'edit' ? '保存' : '签发令牌',
        theme: 'primary',
        loading: savingAgent
      }"
      @confirm="submitAgent"
    >
      <t-form
        :data="agentForm"
        label-width="110px"
        class="agent-dialog-form"
      >
        <t-form-item
          label="代理名称"
          name="agentName"
        >
          <t-input
            v-model="agentForm.agentName"
            placeholder="如 收银台代理 / 后厨代理"
          />
        </t-form-item>
        <t-form-item
          label="授权打印机"
          name="printerIds"
        >
          <t-select
            v-model="agentForm.printerIds"
            :options="printerOptions"
            multiple
            clearable
            placeholder="留空表示授权全部打印机"
          />
          <div class="field-tip">
            留空表示该代理可取走全部打印机的任务；选择后仅授权所选打印机。保存时会自动剔除已删除打印机的残留授权。
          </div>
        </t-form-item>
      </t-form>
    </t-dialog>

    <!-- 一次性令牌回显:关闭后即清空,不可再见 -->
    <t-dialog
      v-model:visible="tokenVisible"
      :header="tokenDialogTitle"
      width="520px"
      :confirm-btn="{ content: '我已保存', theme: 'primary' }"
      :cancel-btn="null"
      :close-on-overlay-click="false"
      :close-on-esc-keydown="false"
      @confirm="closeTokenDialog"
    >
      <div class="token-result">
        <div class="token-warn">请立即复制保存，令牌仅显示这一次。</div>
        <div class="token-row">
          <label>令牌</label>
          <code class="token-value">{{ tokenResult.token }}</code>
          <t-button
            size="small"
            variant="outline"
            @click="copyToken"
          >
            复制
          </t-button>
        </div>
        <div class="token-row">
          <label>前缀</label>
          <span class="token-hint">{{ tokenResult.tokenHint }}</span>
        </div>
        <div class="token-note">关闭此窗口后令牌将无法再次查看，请妥善保存。</div>
        <div
          v-if="tokenResult.token"
          class="install-block"
        >
          <div class="install-title">门店安装命令</div>
          <t-select
            v-model="installPlatform"
            :options="installPlatformOptions"
            style="width: 220px"
          />
          <div class="token-row">
            <label>命令</label>
            <code class="token-value">{{ installCommand }}</code>
            <t-button
              size="small"
              variant="outline"
              @click="copyInstallCommand"
            >
              复制
            </t-button>
          </div>
          <div class="token-note">
            把命令里的产物名换成门店设备对应版本，在门店设备上执行即可，程序会自动配置并开机自启。
          </div>
        </div>
      </div>
    </t-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, watch } from 'vue';
import { MessagePlugin, DialogPlugin } from 'tdesign-vue-next';
import {
  getConfig,
  saveConfig,
  issueAgentToken,
  uploadFile,
  listAgents,
  listPrinters,
  saveAgent,
  setAgentStatus,
  updateAgent,
  deleteAgent,
  previewSample,
  getAgentInfo
} from '../api';
import { useIsMobile } from '../utils/useMobile';
import { hasPerm } from '../utils/perm';
import TicketPreviewDialog from '../components/TicketPreviewDialog.vue';
import type { UploadFile } from 'tdesign-vue-next';
import type { ConfigData, PrintAgent, Printer, TicketPreview } from '../types/entities';

const { isMobile } = useIsMobile();

// 配置项按业务分成四个 tab，避免整页一长串表单项。
const activeTab = ref('basic');

// config:view 可看，config:edit 才能改。只读账号禁用保存按钮(后端 saveConfig 同样 403)。
const canEdit = computed(() => hasPerm('config:edit'));
// 打印代理身份的新增 / 吊销属于打印机管理写操作,沿用 printer:edit 权限码控制显隐。
const canEditAgents = computed(() => hasPerm('printer:edit'));

interface ConfigForm {
  shop_name: string;
  shop_logo: string;
  h5_base_url: string;
  seat_fee_enabled: string;
  seat_fee: number | string;
  promotion_enabled: string;
  promotion_threshold: number | string;
  promotion_discount: number | string;
  pay_qr_wx: string;
  pay_qr_ali: string;
  wxpay_enabled: string;
  alipay_enabled: string;
  wxpay_mchid: string;
  wxpay_appid: string;
  wxpay_apiv3_key: string;
  wxpay_serial_no: string;
  wxpay_private_key_path: string;
  wxpay_platform_cert_path: string;
  wxpay_pubkey_id: string;
  wxpay_pubkey_path: string;
  wxpay_notify_url: string;
  alipay_appid: string;
  alipay_private_key_path: string;
  alipay_public_key: string;
  alipay_notify_url: string;
  print_enabled: string;
  print_kitchen_show_price: string;
  print_guest_footer: string;
  print_guest_show_seat_fee: string;
  print_guest_show_discount: string;
  feie_user: string;
  feie_ukey: string;
  feie_api_url: string;
  agent_token: string;
  agent_latest_version: string;
}

// 后端 config 全量字段:entities 的 ConfigData 只声明了公开配置子集,这里补齐
// 管理端保存 / 回显用到的其余字段(支付参数、打印开关、飞鹅、本地代理等)。
type ConfigPayloadFull = ConfigData & {
  wxpay_mchid?: string;
  wxpay_appid?: string;
  wxpay_apiv3_key?: string;
  wxpay_serial_no?: string;
  wxpay_private_key_path?: string;
  wxpay_platform_cert_path?: string;
  wxpay_pubkey_id?: string;
  wxpay_pubkey_path?: string;
  wxpay_notify_url?: string;
  alipay_appid?: string;
  alipay_private_key_path?: string;
  alipay_public_key?: string;
  alipay_notify_url?: string;
  print_enabled?: string;
  print_kitchen_show_price?: string;
  print_guest_footer?: string;
  print_guest_show_seat_fee?: string;
  print_guest_show_discount?: string;
  feie_user?: string;
  feie_ukey?: string;
  feie_api_url?: string;
  agent_token?: string;
  agent_latest_version?: string;
  // 一次性指令:传 '1' 表示主动清空 agent_token(敏感项留空只表示「不修改」)。
  agent_token_clear?: string;
};

// 拦截器 / axios 抛出的错误统一按此结构读取,只关心文案与 HTTP 状态码。
type HttpError = { message?: string; response?: { status?: number } };

const form = reactive<ConfigForm>({
  shop_name: '',
  shop_logo: '',
  h5_base_url: '',
  seat_fee_enabled: '0',
  seat_fee: '0',
  promotion_enabled: '0',
  promotion_threshold: '0',
  promotion_discount: '0',
  pay_qr_wx: '',
  pay_qr_ali: '',
  wxpay_enabled: '0',
  alipay_enabled: '0',
  wxpay_mchid: '',
  wxpay_appid: '',
  wxpay_apiv3_key: '',
  wxpay_serial_no: '',
  wxpay_private_key_path: '',
  wxpay_platform_cert_path: '',
  wxpay_pubkey_id: '',
  wxpay_pubkey_path: '',
  wxpay_notify_url: '',
  alipay_appid: '',
  alipay_private_key_path: '',
  alipay_public_key: '',
  alipay_notify_url: '',
  print_enabled: '1',
  print_kitchen_show_price: '0',
  print_guest_footer: '谢谢惠顾,欢迎再次光临',
  print_guest_show_seat_fee: '1',
  print_guest_show_discount: '1',
  feie_user: '',
  feie_ukey: '',
  feie_api_url: 'https://api.de.feieyun.com/Api/Open/',
  // 敏感项：后端不回显，留空表示不修改（与飞鹅 UKEY 同一套语义）
  agent_token: '',
  // 普通配置项：随 ping 下发给代理，代理据此提示门店升级；留空不提示。
  agent_latest_version: ''
});
const logoFiles = ref<UploadFile[]>([]);
const wxFiles = ref<UploadFile[]>([]);
const aliFiles = ref<UploadFile[]>([]);
const saving = ref(false);
const loading = ref(true);
// loaded 表示「已成功回显过一次配置」。只有 loaded 为真才允许保存,
// 否则一次误提交就会把线上配置整表覆盖成默认空值(历史事故的成因之一)。
const loaded = ref(false);
// 配置内容指纹(乐观锁):来自后端 ConfigList 的 version,保存时原样带回;
// 库里内容被别人改过后指纹即失配,后端返回 409 拒绝旧快照的全量覆盖。
const version = ref('');

// 开关型字段:t-switch 的 custom-value 只认 '1' / '0'。
// 若绑定值出现空串或意外取值,组件会在更新阶段抛
// `value is not in ["1","0"]`,该异常会打断整棵路由树的 patch ——
// 现象就是「打开过系统配置页后,再点其他菜单全部白屏」。
// 因此回显前一律归一化,不要相信后端/历史数据的取值。
const FLAG_KEYS = [
  'seat_fee_enabled',
  'promotion_enabled',
  'wxpay_enabled',
  'alipay_enabled',
  'print_enabled',
  'print_kitchen_show_price',
  'print_guest_show_seat_fee',
  'print_guest_show_discount'
] as const;

function normFlag(v: unknown): '1' | '0' {
  return String(v ?? '') === '1' ? '1' : '0';
}

// silent = true 表示保存后的静默回显:不切换 loading 态(避免表单卸载重挂的闪烁),
// 失败时也保留当前已加载状态。
async function load(silent = false): Promise<void> {
  if (!silent) {
    loading.value = true;
    loaded.value = false;
  }
  try {
    const res = (await getConfig()) as ConfigPayloadFull;
    Object.assign(form, res);
    form.shop_logo = res.shop_logo || '';
    // 新加的文本项对老后端(未下发该字段)兜底为空串,避免 undefined 绑定到输入框。
    form.print_guest_footer = res.print_guest_footer ?? '';
    // 记录当前配置指纹,保存时带回做并发冲突校验(乐观锁)
    version.value = res.version || '';
    // 数值字段转为数字,便于 t-input-number 绑定
    form.seat_fee = Number(res.seat_fee) || 0;
    form.promotion_threshold = Number(res.promotion_threshold) || 0;
    form.promotion_discount = Number(res.promotion_discount) || 0;
    // 开关字段归一化为 '1' / '0'
    FLAG_KEYS.forEach(k => {
      form[k] = normFlag(res[k]);
    });
    loaded.value = true;
  } catch (e) {
    MessagePlugin.error((e as HttpError | null | undefined)?.message || '配置加载失败，请稍后重试');
  } finally {
    if (!silent) loading.value = false;
  }
}

async function save(): Promise<void> {
  if (!canEdit.value) {
    MessagePlugin.warning('当前账号只能查看配置，不能修改');
    return;
  }
  if (!loaded.value) {
    MessagePlugin.warning('配置尚未加载成功，请先重新加载');
    return;
  }
  saving.value = true;
  try {
    // 三个上传位走同一路径:raw 是真正的 File 才上传(t-upload 回显项只有 url 没有 raw,
    // 不能把整个 UploadFile 对象当 File 塞进 FormData)。
    // Logo 此前漏了保存处理:用户上传后点保存图片静默丢失,现补齐。
    if (logoFiles.value.length) {
      const f = logoFiles.value[0].raw;
      if (f instanceof File) form.shop_logo = (await uploadFile(f)).url;
    }
    if (wxFiles.value.length) {
      const f = wxFiles.value[0].raw;
      if (f instanceof File) form.pay_qr_wx = (await uploadFile(f)).url;
    }
    if (aliFiles.value.length) {
      const f = aliFiles.value[0].raw;
      if (f instanceof File) form.pay_qr_ali = (await uploadFile(f)).url;
    }
    // 数值字段转回字符串,与后端约定一致;开关字段兜底为 '1'/'0'
    const payload: ConfigPayloadFull = {
      ...form,
      seat_fee: String(form.seat_fee),
      promotion_threshold: String(form.promotion_threshold),
      promotion_discount: String(form.promotion_discount),
      // 乐观锁:带回回显时的指纹,后端发现不一致(有其他人已先保存)会拒绝
      version: version.value
    };
    FLAG_KEYS.forEach(k => {
      payload[k] = normFlag(form[k]);
    });
    await saveConfig(payload);
    MessagePlugin.success('保存成功');
    // 保存后重新拉取,保证界面与库里的真实值一致。
    // 走 silent:非静默会短暂卸载整张表单再挂回,用户当前 tab 与滚动位置被重置。
    await load(true);
    // 代理令牌可能刚被修改:刷新运行状态,让「在线/离线」跟着真实配置走。
    loadAgentStatus();
    // 代理令牌是敏感项、后端不回显:保存后清空输入框,避免它一直明文挂在页面上,
    // 也避免下次保存时把同一个值再提交一遍(虽然后端是幂等的,但看着容易误解)。
    form.agent_token = '';
    // 清空指令是一次性的:已随本次保存生效,不能留在表单里影响后续保存。
    agentTokenClear.value = false;
  } catch (e) {
    // 409:配置已被他人修改,后端已拒绝(拦截器已弹出提示)。静默拉取最新值
    // 回填表单,让用户基于最新内容核对后再次保存,而不是拿着过期表单反复撞墙。
    if ((e as HttpError | null | undefined)?.response?.status === 409) {
      await load(true);
      return;
    }
    MessagePlugin.error((e as HttpError | null | undefined)?.message || '保存失败，请稍后重试');
  } finally {
    saving.value = false;
  }
}

// 签发全局代理令牌。
//
// 令牌改由后端生成并直接落库(加密),不再走「前端生成 → 随整份配置保存提交」:
// 后者漏点一次「保存配置」就会出现「门店代理填了令牌、云端却从未存过它」的哑火状态,
// 而敏感项不回显、输入框恒为空,界面上完全看不出差别,只能靠代理一直报「令牌不正确」发现。
// 现在点一次即生效,明文只在回显弹窗里出现一次(库里只有密文、关窗后再不可见)。
const issuingToken = ref(false);

async function genAgentToken(): Promise<void> {
  if (issuingToken.value) return;
  if (!canEdit.value) {
    MessagePlugin.warning('当前账号只能查看配置，不能修改');
    return;
  }
  issuingToken.value = true;
  try {
    const res = await issueAgentToken();
    // 库里的令牌变了,配置指纹随之变化:必须同步刷新本地 version,
    // 否则紧接着点「保存配置」会因指纹失配被 409 拦下,而 409 分支用后端数据
    // 整表覆盖表单,会把页面上还没保存的其它改动一起冲掉。
    if (res.version) version.value = res.version;
    tokenResult.value = {
      token: res.token || '',
      tokenHint: res.tokenHint || '',
      agentId: null,
      scope: 'global'
    };
    tokenVisible.value = true;
    // 令牌已配置:刷新「在线/离线」与队列概况。
    loadAgentStatus();
  } catch {
    // 失败原因由请求拦截器统一 toast
  } finally {
    issuingToken.value = false;
  }
}

// 清空全局代理令牌的「待保存标记」。
//
// agent_token 是敏感项:后端不回显,输入框留空的语义是「本次不修改」,
// 光靠清空输入框无法表达「停用这条令牌」。所以这里只打标记,随下一次保存提交,
// 由后端把库里的值置空。
const agentTokenClear = ref(false);

function onClearAgentToken(): void {
  if (!canEdit.value) {
    MessagePlugin.warning('当前账号只能查看配置，不能修改');
    return;
  }
  if (!agentStatus.configured) {
    MessagePlugin.info('当前未配置代理令牌，无需清空');
    return;
  }
  const dlg = DialogPlugin.confirm({
    header: '确认清空代理令牌',
    theme: 'warning',
    body: '清空后使用全局令牌的旧代理会立即失联（用逐台签发令牌的 v2 代理不受影响）。若仍需出纸，请逐台重新配置或改签独立身份。确认后需点击页面底部「保存配置」才会生效。',
    onConfirm: () => {
      form.agent_token = '';
      agentTokenClear.value = true;
      MessagePlugin.success('已标记清空，点击「保存配置」后生效');
      dlg.hide();
    }
  });
}

// ============ 票据模板示例预览 ============
// 用示例数据 + 当前表单值(未保存)渲染「预计打印模板」,所见即所改。
const sampleVisible = ref(false);
const sampleLoading = ref(false);
const sampleData = ref<TicketPreview | null>(null);
const sampleDocType = ref<'guest' | 'kitchen'>('guest');
const samplePaperWidth = ref<number>(48);

const sampleTitle = computed(() => {
  const doc = sampleDocType.value === 'kitchen' ? '厨房单' : '食客小票';
  return `模板预览 · ${doc}(示例数据)`;
});

async function loadSamplePreview(): Promise<void> {
  sampleVisible.value = true;
  sampleLoading.value = true;
  try {
    // 表单当前值作为覆盖参数:页脚/开关改了没保存,预览里也立刻反映。
    sampleData.value = await previewSample({
      docType: sampleDocType.value,
      paperWidth: samplePaperWidth.value,
      footer: form.print_guest_footer,
      showSeatFee: form.print_guest_show_seat_fee === '1',
      showDiscount: form.print_guest_show_discount === '1',
      kitchenShowPrice: form.print_kitchen_show_price === '1'
    });
  } catch {
    // 失败原因由请求拦截器统一弹出
    sampleVisible.value = false;
  } finally {
    sampleLoading.value = false;
  }
}

// ============ 代理运行状态(在线/心跳/积压) ============
// 来自 GET /admin/printer/agent/info:覆盖全局令牌(legacy)与逐台签发(v2)两种形态,
// 老后端无此接口时静默降级为「未配置」,不影响整页配置功能。
const agentStatus = reactive({
  configured: false,
  online: false,
  lastSeen: '',
  pending: 0,
  dead: 0,
  oldestPendingSec: 0
});

async function loadAgentStatus(): Promise<void> {
  try {
    const res = await getAgentInfo();
    agentStatus.configured = !!res.configured;
    agentStatus.online = !!res.online;
    agentStatus.lastSeen = res.lastSeen || '';
    agentStatus.pending = Number(res.pending) || 0;
    agentStatus.dead = Number(res.dead) || 0;
    agentStatus.oldestPendingSec = Number(res.oldestPendingSec) || 0;
  } catch {
    agentStatus.configured = false;
  }
}

// 与后端 agentOnlineWindow(90s)一致的在线判定:最近心跳距今 < 90 秒。
// lastSeen 为「YYYY-MM-DD HH:mm:ss」本地时间,补 T 后由浏览器按本地时区解析。
function isAgentOnline(row: PrintAgent): boolean {
  const seen = row.lastSeen || '';
  if (!seen) return false;
  const t = new Date(seen.replace(' ', 'T')).getTime();
  return !Number.isNaN(t) && Date.now() - t < 90_000;
}

// 最老待打印单已等分钟数(向上取整);老后端没有 oldestPendingSec 时为 0。
const agentWaitMinutes = computed(() => Math.ceil((agentStatus.oldestPendingSec || 0) / 60));

// ============ 打印代理身份管理(per-agent 令牌) ============
const agents = ref<PrintAgent[]>([]);
const printers = ref<Printer[]>([]);
const agentDialogVisible = ref(false);
const savingAgent = ref(false);
// 同一个弹窗承担两种模式:add 签发新令牌,edit 只改名称与授权范围。
const agentDialogMode = ref<'add' | 'edit'>('add');
interface AgentForm {
  agentId: number | null;
  agentName: string;
  printerIds: number[];
}
const agentForm = reactive<AgentForm>({ agentId: null, agentName: '', printerIds: [] });
// 一次性令牌回显:只在签发成功后的弹窗里存在,关闭即清空,不可再见。
// scope 区分「全局令牌」与「逐台代理身份」,两者复用同一个回显弹窗但文案不同。
const tokenVisible = ref(false);
interface TokenResult {
  token: string;
  tokenHint: string;
  agentId: number | null;
  scope: 'global' | 'agent';
}
const emptyTokenResult = (): TokenResult => ({ token: '', tokenHint: '', agentId: null, scope: 'agent' });
const tokenResult = ref<TokenResult>(emptyTokenResult());
const tokenDialogTitle = computed(() =>
  tokenResult.value.scope === 'global' ? '全局代理令牌已签发' : '代理令牌已生成'
);

// 门店安装命令:按平台给出对应产物名,拼出「一条命令安装」。
const installPlatforms = [
  { key: 'darwin-amd64', label: 'macOS Intel', artifact: 'print-agent-darwin-amd64' },
  { key: 'darwin-arm64', label: 'macOS Apple Silicon', artifact: 'print-agent-darwin-arm64' },
  { key: 'windows-amd64', label: 'Windows', artifact: 'print-agent-windows-amd64.exe' },
  { key: 'linux-amd64', label: 'Linux x86_64', artifact: 'print-agent-linux-amd64' },
  { key: 'linux-arm64', label: 'Linux ARM', artifact: 'print-agent-linux-arm64' }
];
const installPlatform = ref('windows-amd64');
const installPlatformOptions = installPlatforms.map(p => ({ label: p.label, value: p.key }));
const installOrigin = computed(() => (window.location.origin || '').replace(/\/+$/, ''));
const installCommand = computed(() => {
  const t = tokenResult.value.token;
  if (!t) return '';
  const p = installPlatforms.find(x => x.key === installPlatform.value) || installPlatforms[0];
  return `${p.artifact} --install --server ${installOrigin.value} --token ${t}`;
});

const printerOptions = computed(() =>
  printers.value.map(p => ({
    label: p.printerName || `打印机 #${p.printerId}`,
    value: p.printerId
  }))
);

function printerName(id: number): string {
  const p = printers.value.find(x => x.printerId === id);
  if (!p) return `#${id}`;
  return p.printerName ?? '';
}

function printerScopeText(row: PrintAgent): string {
  const ids: number[] =
    Array.isArray(row.printerIdList) && row.printerIdList.length
      ? row.printerIdList
      : String(row.printerIds || '')
          .split(',')
          .map(s => Number(s))
          .filter(n => n > 0);
  if (!ids.length) return '全部打印机';
  return ids.map(printerName).join('、');
}

// 老后端无这三个接口时,listAgents 会 404;已用 silent 请求,失败静默置空,
// 不影响整页配置的加载与保存。
async function loadAgents(): Promise<void> {
  try {
    const res = await listAgents();
    agents.value = (res && res.items) || [];
  } catch {
    agents.value = [];
  }
}

async function loadPrinters(): Promise<void> {
  // 没有打印机查看权限就不去拉列表,避免打开配置页时被 403 toast 打扰;
  // 授权范围届时回退显示打印机 id,新增弹窗的多选留空表示授权全部。
  if (!hasPerm('printer:view')) {
    printers.value = [];
    return;
  }
  try {
    const res = await listPrinters();
    printers.value = (res && res.items) || [];
  } catch {
    printers.value = [];
  }
}

function openAgentAdd(): void {
  agentDialogMode.value = 'add';
  agentForm.agentId = null;
  agentForm.agentName = '';
  agentForm.printerIds = [];
  agentDialogVisible.value = true;
}

// 编辑只改名称与授权范围:令牌不变,门店侧无需重新安装或改配置。
function openAgentEdit(row: PrintAgent): void {
  agentDialogMode.value = 'edit';
  agentForm.agentId = row.agentId ?? null;
  agentForm.agentName = row.agentName || '';
  agentForm.printerIds = agentPrinterIds(row);
  agentDialogVisible.value = true;
}

// 授权范围回显:优先用后端补算的数组,老后端只有 CSV 字符串时按逗号拆。
function agentPrinterIds(row: PrintAgent): number[] {
  if (Array.isArray(row.printerIdList) && row.printerIdList.length) return [...row.printerIdList];
  return String(row.printerIds || '')
    .split(',')
    .map(s => Number(s))
    .filter(n => n > 0);
}

async function submitAgent(): Promise<void> {
  if (savingAgent.value) return;
  const name = (agentForm.agentName || '').trim();
  if (!name) {
    MessagePlugin.warning('请填写代理名称');
    return;
  }
  savingAgent.value = true;
  try {
    // 后端以 CSV 字符串接收;空数组 = 授权全部打印机。
    const printerIds = (agentForm.printerIds || []).join(',');
    if (agentDialogMode.value === 'edit') {
      await updateAgent(agentForm.agentId ?? 0, { agentName: name, printerIds });
      agentDialogVisible.value = false;
      MessagePlugin.success('已保存');
      loadAgents();
      return;
    }
    const res = await saveAgent({ agentName: name, printerIds });
    agentDialogVisible.value = false;
    tokenResult.value = {
      token: res.token || '',
      tokenHint: res.tokenHint || '',
      agentId: res.agentId ?? null,
      scope: 'agent'
    };
    tokenVisible.value = true;
    loadAgents();
  } catch {
    // 失败时拦截器已提示,保持弹窗打开让用户修改后重试。
  } finally {
    savingAgent.value = false;
  }
}

// 删除已吊销的代理身份:清理历史记录与其令牌哈希。
function onDeleteAgent(row: PrintAgent): void {
  const dlg = DialogPlugin.confirm({
    header: '确认删除代理',
    theme: 'warning',
    body: `确认删除代理【${row.agentName}】？该记录与其令牌将被移除且不可恢复；${
      row.tokenHint ? `令牌前缀 ${row.tokenHint} 立即失效。` : ''
    }如需停用但保留记录，请用「吊销」。`,
    onConfirm: async () => {
      try {
        await deleteAgent(row.agentId ?? 0);
        MessagePlugin.success('已删除');
        loadAgents();
      } catch {
        /* 失败已由拦截器统一 toast */
      } finally {
        dlg.hide();
      }
    }
  });
}

function onToggleAgentStatus(row: PrintAgent): void {
  const enabling = row.status !== 1;
  const dlg = DialogPlugin.confirm({
    header: enabling ? '确认恢复' : '确认吊销',
    theme: enabling ? 'default' : 'warning',
    body: enabling
      ? `确认恢复代理【${row.agentName}】？恢复后该代理可用原令牌继续取单。`
      : `确认吊销代理【${row.agentName}】？吊销后该代理的令牌立即失效，无法再取单。`,
    onConfirm: async () => {
      try {
        await setAgentStatus(row.agentId ?? 0, enabling ? 1 : 0);
        MessagePlugin.success(enabling ? '已恢复' : '已吊销');
        loadAgents();
      } catch {
        /* 失败已由拦截器统一 toast */
      } finally {
        // DialogPlugin 不会自动关闭,成功失败都要收起(否则报错时弹窗会卡住)。
        dlg.hide();
      }
    }
  });
}

function closeTokenDialog(): void {
  tokenVisible.value = false;
}

function clearTokenResult(): void {
  tokenResult.value = emptyTokenResult();
}

// 无论通过「我已保存」、右上角 X 还是其它路径关闭,只要弹窗关闭就清空一次性令牌,
// 保证关闭后不可再见。
watch(tokenVisible, v => {
  if (!v) clearTokenResult();
});

async function copyToken(): Promise<void> {
  const t = tokenResult.value.token;
  if (!t) return;
  try {
    if (navigator.clipboard && typeof navigator.clipboard.writeText === 'function') {
      await navigator.clipboard.writeText(t);
    } else {
      throw new Error('clipboard unavailable');
    }
    MessagePlugin.success('已复制到剪贴板');
  } catch {
    MessagePlugin.warning('复制失败，请手动选中并复制令牌');
  }
}

async function copyInstallCommand(): Promise<void> {
  const cmd = installCommand.value;
  if (!cmd) return;
  try {
    if (navigator.clipboard && typeof navigator.clipboard.writeText === 'function') {
      await navigator.clipboard.writeText(cmd);
    } else {
      throw new Error('clipboard unavailable');
    }
    MessagePlugin.success('已复制到剪贴板');
  } catch {
    MessagePlugin.warning('复制失败，请手动选中并复制命令');
  }
}

onMounted(() => {
  load();
  loadAgents();
  loadPrinters();
  loadAgentStatus();
});
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

/* 分组 tab:内容区与导航条之间留白 */
.cfg-tabs :deep(.t-tabs__nav) {
  margin-bottom: 20px;
}

/* tab 顶部业务提示条 */
.tab-note {
  margin-bottom: 18px;
  padding: 9px 12px;
  background: var(--brand-soft);
  color: var(--brand-deep);
  border-radius: var(--r-sm);
  font-size: 12px;
  line-height: 1.6;
}

/* tab 内子分组标题(微信支付 / 支付宝支付) */
.cfg-subhead {
  margin: 4px 0 12px;
  padding-left: 10px;
  border-left: 3px solid var(--brand);
  font-size: 14px;
  font-weight: 600;
  color: var(--ink-2);
  line-height: 1.4;
}

/* 底部保存栏:与 tabs 分隔,任意 tab 内编辑后都能直接保存 */
.cfg-footer {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 4px;
  padding-top: 16px;
  border-top: 1px solid var(--line);
}

.field-tip {
  margin-top: 6px;
  font-size: 12px;
  color: #999;
  line-height: 1.6;
}

/* 「新增打印代理」弹窗:说明文字跟在多选框右侧会被 flex 挤成窄列,让它换行独占一行 */
.agent-dialog-form :deep(.t-form__controls-content) {
  flex-wrap: wrap;
}

.agent-dialog-form :deep(.field-tip) {
  flex: 0 0 100%;
}

.agent-mgr {
  width: 100%;
}

.agent-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 10px;
}

.agent-title {
  font-size: 13px;
  color: #666;
}

.agent-empty {
  padding: 18px 0;
  color: #bbb;
  font-size: 13px;
  text-align: center;
  background: #fafafa;
  border-radius: 6px;
}

.agent-card {
  border: 1px solid #e7e7e7;
  border-radius: 8px;
  padding: 12px 14px;
  margin-bottom: 10px;
  background: #fff;
}

/* tab 顶部的「代理运行状态」卡片:与下方表单项保持间距 */
.agent-status {
  margin-bottom: 18px;
}

.agent-card-hd {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-bottom: 10px;
}

.agent-name {
  font-size: 14px;
  font-weight: 600;
  color: #333;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.agent-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px 16px;
}

.agent-cell .k {
  font-size: 12px;
  color: #999;
  margin-bottom: 2px;
}

.agent-cell .v {
  font-size: 13px;
  color: #333;
  word-break: break-all;
  line-height: 1.5;
}

.agent-cell code,
.token-value {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  background: #f5f5f5;
  padding: 2px 6px;
  border-radius: 4px;
  font-size: 12px;
  word-break: break-all;
}

.agent-card-ft {
  margin-top: 10px;
  padding-top: 8px;
  border-top: 1px dashed #eee;
  display: flex;
  justify-content: flex-end;
}

.token-result {
  display: flex;
  flex-direction: column;
  gap: 14px;
  padding: 4px 0 2px;
}

.token-warn {
  font-size: 13px;
  color: #e37318;
  line-height: 1.6;
}

.token-row {
  display: flex;
  align-items: flex-start;
  gap: 10px;
}

.token-row label {
  width: 44px;
  font-size: 13px;
  color: #666;
  flex-shrink: 0;
  line-height: 28px;
}

.token-value {
  flex: 1;
  min-width: 0;
  line-height: 1.6;
}

.token-hint {
  font-size: 13px;
  color: #333;
  line-height: 28px;
}

.token-note {
  font-size: 12px;
  color: #999;
}

.install-block {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding-top: 12px;
  border-top: 1px dashed #eee;
}

.install-title {
  font-size: 13px;
  font-weight: 600;
  color: #333;
}

/* 移动端:Logo / 收款码的「图片 + 上传框」并排会挤爆,改为上下堆叠 */
@media (width <= 767px) {
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

  .agent-grid {
    grid-template-columns: 1fr;
  }
}
</style>
