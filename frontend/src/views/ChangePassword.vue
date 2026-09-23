<template>
  <div class="cp-wrap">
    <div class="page-card cp-card">
      <div class="cp-title">修改密码</div>
      <div class="cp-sub">修改成功后，当前登录将失效，需用新密码重新登录</div>

      <div class="cp-row">
        <label>原密码</label>
        <t-input
          v-model="form.oldPassword"
          type="password"
          placeholder="请输入当前密码"
          autocomplete="current-password"
        />
      </div>
      <div class="cp-row">
        <label>新密码</label>
        <t-input
          v-model="form.newPassword"
          type="password"
          placeholder="至少 6 位"
          autocomplete="new-password"
        />
      </div>
      <div class="cp-row">
        <label>确认新密码</label>
        <t-input
          v-model="form.confirm"
          type="password"
          placeholder="再次输入新密码"
          autocomplete="new-password"
          @keyup.enter="submit"
        />
      </div>

      <button
        class="cp-btn"
        :disabled="submitting"
        @click="submit"
      >
        <span
          v-if="submitting"
          class="spin light"
        ></span>
        {{ submitting ? '提交中…' : '确认修改' }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue';
import { useRouter } from 'vue-router';
import { MessagePlugin } from 'tdesign-vue-next';
import { changePassword } from '../api';
import { clearAuth } from '../utils/perm';
import { clearRemember } from '../utils/remember';

const router = useRouter();
const submitting = ref(false);
const form = reactive({ oldPassword: '', newPassword: '', confirm: '' });

async function submit(): Promise<void> {
  if (!form.oldPassword) {
    MessagePlugin.warning('请输入原密码');
    return;
  }
  if (form.newPassword.length < 6) {
    MessagePlugin.warning('新密码至少 6 位');
    return;
  }
  if (form.newPassword !== form.confirm) {
    MessagePlugin.warning('两次输入的新密码不一致');
    return;
  }
  submitting.value = true;
  try {
    await changePassword({ oldPassword: form.oldPassword, newPassword: form.newPassword });
    // 后端改密时会把 token_version +1,当前令牌随即失效 —— 与其等下一个请求
    // 撞 401 被拦截器兜走,不如在这里主动清登录态并引导重新登录,提示更明确。
    // 后端改密同时作废了该账号全部「记住我」令牌(所有设备),这里清本地即可。
    clearAuth();
    clearRemember();
    MessagePlugin.success('密码修改成功，请用新密码重新登录');
    router.replace('/login');
  } finally {
    submitting.value = false;
  }
}
</script>

<style scoped>
.cp-wrap {
  display: flex;
  justify-content: center;
  padding-top: 24px;
}

.cp-card {
  width: 420px;
  max-width: 100%;
}

.cp-title {
  font-size: 16px;
  font-weight: 700;
  color: var(--ink);
}

.cp-sub {
  font-size: 12.5px;
  color: var(--ink-3);
  margin: 6px 0 22px;
}

.cp-row {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 16px;
}

.cp-row label {
  width: 72px;
  font-size: 13px;
  color: var(--ink-2);
  flex-shrink: 0;
}

.cp-row :deep(.t-input) {
  flex: 1;
}

.cp-btn {
  width: 100%;
  height: 44px;
  margin-top: 8px;
  border: none;
  border-radius: 10px;
  background: var(--grad-brand);
  color: #fff;
  font-size: 14px;
  font-weight: 600;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  cursor: pointer;
  transition: opacity 0.15s;
}

.cp-btn:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

.cp-btn .spin {
  display: inline-block;
  width: 13px;
  height: 13px;
  border: 2px solid rgb(255 255 255 / 40%);
  border-top-color: #fff;
  border-radius: 50%;
  animation: cpspin 0.8s linear infinite;
}

@keyframes cpspin {
  to {
    transform: rotate(360deg);
  }
}

@media (width <= 767px) {
  .cp-wrap {
    padding-top: 8px;
  }

  .cp-row {
    flex-direction: column;
    align-items: stretch;
    gap: 6px;
  }
}
</style>
