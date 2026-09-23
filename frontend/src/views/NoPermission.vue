<template>
  <div class="page-card np-card">
    <div class="np-icon">
      <lock-on-icon />
    </div>
    <div class="np-title">当前账号没有可用功能</div>
    <div class="np-desc">
      账号 <b>{{ displayName }}</b>
      <template v-if="roleName"> （角色：{{ roleName }}） </template>
      未被分配任何可访问的菜单权限。<br />
      请让管理员在「员工管理 / 角色权限」里为该账号的角色勾选所需权限。
    </div>
    <div class="np-acts">
      <t-button
        theme="default"
        variant="outline"
        @click="onLogout"
      >
        退出登录
      </t-button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue';
import { useRouter } from 'vue-router';
import { clearAuth, getDisplayName, getRoleName } from '../utils/perm';
import { revokeRemember } from '../utils/remember';

const router = useRouter();
const displayName = computed(() => getDisplayName());
const roleName = computed(() => getRoleName());

function onLogout() {
  clearAuth();
  // 同 AdminLayout:吊销本设备的「记住我」令牌,而非只清本地。
  revokeRemember();
  router.replace('/login');
}
</script>

<style scoped>
/* 兜底页:账号一个可用菜单都没有时展示,避免「空白布局」式的白屏 */
.np-card {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  text-align: center;
  padding: 64px 24px;
  min-height: 360px;
}

.np-icon {
  width: 56px;
  height: 56px;
  border-radius: 16px;
  background: var(--brand-soft);
  color: var(--brand-deep);
  font-size: 26px;
  display: flex;
  align-items: center;
  justify-content: center;
  margin-bottom: 18px;
}

.np-title {
  font-size: 17px;
  font-weight: 700;
  color: var(--ink);
}

.np-desc {
  font-size: 13px;
  color: var(--ink-3);
  line-height: 1.9;
  margin: 10px 0 22px;
  max-width: 520px;
}

.np-desc b {
  color: var(--ink-2);
}
</style>
