<template>
  <div
    class="review-mask"
    @click.self="emit('close')"
  >
    <div class="review-sheet">
      <div class="review-head">
        <span class="review-title">这顿吃得如何？</span>
        <span
          class="review-close"
          @click="emit('close')"
          >×</span
        >
      </div>
      <div class="review-sub">您的反馈将帮助我们改进出品与服务</div>

      <!-- 星级:五颗大热区按钮,必填。点选后下方标签组按好/差联动 -->
      <div class="review-stars">
        <button
          v-for="n in 5"
          :key="n"
          type="button"
          class="review-star"
          :class="{ on: n <= rating }"
          :aria-label="n + '星'"
          @click="setRating(n)"
        >
          ★
        </button>
      </div>
      <div class="review-rating-text">{{ ratingText }}</div>

      <!-- 快捷标签:按星级联动(>=4 好评组、<=2 差评组、3 星两组都给)。
           切换星级时清空已选,避免「3 星 + 味道好」这类矛盾组合 -->
      <div
        v-if="visibleTags.length"
        class="review-tags"
      >
        <span
          v-for="t in visibleTags"
          :key="t"
          class="review-chip"
          :class="{ active: selected.includes(t) }"
          @click="toggleTag(t)"
          >{{ t }}</span
        >
      </div>

      <!-- 图片(选填,最多 3 张):先传后评 —— 选图即上传到独立表,提交时按名绑定;
           失败的单张标红可重试,不打断其它图 -->
      <div class="review-imgs">
        <div
          v-for="(img, i) in images"
          :key="img.name"
          class="review-img"
        >
          <img
            :src="img.preview"
            alt="评价图片"
          />
          <span
            v-if="img.uploading"
            class="review-img-loading"
            >…</span
          >
          <span
            v-else-if="img.error"
            class="review-img-error"
            @click="retryUpload(i)"
            >!</span
          >
          <span
            class="review-img-del"
            @click="removeImage(i)"
            >×</span
          >
        </div>
        <label
          v-if="images.length < MAX_IMAGES"
          class="review-img-add"
        >
          +
          <input
            type="file"
            accept="image/jpeg,image/png,image/gif,image/webp"
            multiple
            @change="onPickImages"
          />
        </label>
      </div>

      <textarea
        :value="content"
        class="review-content"
        rows="3"
        maxlength="200"
        placeholder="想说点什么？(选填)"
        @input="onContentInput"
      ></textarea>
      <div class="review-count">{{ content.length }}/200</div>

      <button
        type="button"
        class="review-submit"
        :class="{ disabled: !rating || submitting || anyUploading }"
        @click="rating && !submitting && !anyUploading && emit('submit', { rating, tags: selected, content, images: imageNames })"
      >
        {{ anyUploading ? '图片上传中…' : submitting ? '提交中…' : '提交评价' }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue';
import { uploadReviewImage } from '@dining/api';

const props = defineProps<{
  /** 提交进行中:按钮锁定,防弱网下连点 */
  submitting?: boolean;
}>();

const emit = defineEmits<{
  close: [];
  submit: [payload: { rating: number; tags: string[]; content: string; images: string[] }];
}>();

// 快捷标签固定两组(v1 不做商户自定义,见 spec §0.2)。七个正交维度,好/差一一对称:
// 标签云的核心价值是「同维度好/差对比」(味道好 20 次 vs 不合口味 15 次),
// 不对称的词会让聚合失效;差评侧用顾客自然口语且可行动(老板看得懂该改什么)。
const GOOD_TAGS = ['味道好', '上菜快', '分量足', '服务热情', '环境舒适', '干净卫生', '实惠'];
const BAD_TAGS = ['不合口味', '上菜慢', '分量少', '服务差', '环境差', '不够干净', '偏贵'];

// 图片:先传后评,最多 3 张(po.ReviewImagesMax)。预览用本地 objectURL,
// 上传成功后记录服务端文件名(name),提交时只交名字。
const MAX_IMAGES = 3;
interface ReviewImg {
  name: string; // 服务端文件名(上传成功后填)
  preview: string; // 本地预览 URL
  uploading: boolean;
  error: boolean;
  file?: File;
}
const images = ref<ReviewImg[]>([]);
const anyUploading = computed(() => images.value.some(i => i.uploading));
const imageNames = computed(() => images.value.filter(i => i.name && !i.error).map(i => i.name));

async function uploadOne(idx: number): Promise<void> {
  const img = images.value[idx];
  if (!img?.file) return;
  img.uploading = true;
  img.error = false;
  try {
    const res = await uploadReviewImage(img.file);
    img.name = res.url.split('/').pop() || '';
  } catch {
    img.error = true; // 保留行,标红可重试
  } finally {
    img.uploading = false;
  }
}

function onPickImages(e: Event): void {
  const input = e.target as HTMLInputElement;
  const files = Array.from(input.files || []);
  input.value = ''; // 允许重复选择同一文件
  for (const f of files) {
    if (images.value.length >= MAX_IMAGES) break;
    if (!f.type.startsWith('image/')) continue;
    images.value.push({ name: '', preview: URL.createObjectURL(f), uploading: false, error: false, file: f });
    void uploadOne(images.value.length - 1);
  }
}

function retryUpload(idx: number): void {
  void uploadOne(idx);
}

function removeImage(idx: number): void {
  const [rm] = images.value.splice(idx, 1);
  if (rm?.preview) URL.revokeObjectURL(rm.preview);
}

// 组件销毁(弹层关闭/提交完成)时回收本地预览的 objectURL
onUnmounted(() => {
  for (const img of images.value) {
    if (img.preview) URL.revokeObjectURL(img.preview);
  }
});

const rating = ref(0);
const selected = ref<string[]>([]);
const content = ref('');

// 星级 → 标签组:好评组 / 差评组 / 中评两组都给;0 与越界给空(非法输入不给标签组)。
// 切换星级清空已选,避免「2 星 + 味道好」的矛盾组合落库。
const visibleTags = computed(() => {
  if (rating.value === 4 || rating.value === 5) return GOOD_TAGS;
  if (rating.value === 1 || rating.value === 2) return BAD_TAGS;
  if (rating.value === 3) return [...GOOD_TAGS, ...BAD_TAGS];
  return [];
});
watch(rating, () => {
  selected.value = [];
});

function setRating(n: number): void {
  rating.value = n;
}

function toggleTag(t: string): void {
  const i = selected.value.indexOf(t);
  if (i >= 0) selected.value = selected.value.filter(x => x !== t);
  else if (selected.value.length < 6) selected.value = [...selected.value, t];
}

function onContentInput(e: Event): void {
  content.value = (e.target as HTMLTextAreaElement).value;
}

const ratingText = computed(() => ['', '很不满意', '不太满意', '一般', '满意', '非常满意'][rating.value] || '点星星评分');
</script>

<style scoped>
/* 层级与 RemarkSheet 同款:必须高过购物车抽屉(.t-drawer z-index 1500) */
.review-mask {
  position: fixed;
  inset: 0;
  background: rgb(0 0 0 / 50%);
  z-index: 1600;
  display: flex;
  align-items: flex-end;
  justify-content: center;
}

.review-sheet {
  width: 100%;
  max-width: 480px;
  background: #fff;
  border-radius: var(--r-lg) var(--r-lg) 0 0;
  padding: 20px 18px calc(20px + env(safe-area-inset-bottom));
}

.review-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.review-title {
  font-size: 15px;
  font-weight: 700;
  color: var(--ink);
}

.review-close {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  margin-right: -6px;
  color: var(--ink-4);
  font-size: 20px;
  line-height: 1;
  cursor: pointer;
}

.review-sub {
  margin-top: 2px;
  font-size: 12.5px;
  color: var(--ink-3);
}

/* 星级:56px 大热区按钮,手指点得中;未选中灰色轮廓,选中金色实心 */
.review-stars {
  display: flex;
  justify-content: center;
  gap: 6px;
  margin-top: 14px;
}

.review-star {
  width: 52px;
  height: 52px;
  border: none;
  background: none;
  font-size: 36px;
  line-height: 1;
  color: #e5e5e5;
  cursor: pointer;
  padding: 0;
}

.review-star.on {
  color: #ffb400;
}

.review-rating-text {
  text-align: center;
  margin-top: 2px;
  font-size: 13px;
  color: var(--ink-3);
  min-height: 18px;
}

.review-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 12px;
  justify-content: center;
}

.review-chip {
  padding: 7px 16px;
  border-radius: var(--r-pill);
  font-size: 13px;
  cursor: pointer;
  transition: all 0.12s;
  border: 1.5px solid var(--line);
  background: #fafafa;
  color: var(--ink-2);
}

.review-chip.active {
  border-color: var(--brand);
  background: var(--brand-soft);
  color: var(--brand-deep);
  font-weight: 600;
}

.review-content {
  width: 100%;
  box-sizing: border-box;
  margin-top: 14px;
  padding: 10px 14px;
  border: 1.5px solid var(--line);
  border-radius: var(--r-md);
  font-size: 13.5px;
  color: var(--ink);
  outline: none;
  resize: none;
  font-family: inherit;
}

/* 图片网格:缩略图 72px 方块,三列 */
.review-imgs {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 14px;
}

.review-img {
  position: relative;
  width: 72px;
  height: 72px;
  border-radius: var(--r-md);
  overflow: hidden;
  border: 1px solid var(--line);
}

.review-img img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.review-img-loading,
.review-img-error {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgb(0 0 0 / 45%);
  color: #fff;
  font-size: 13px;
}

.review-img-error {
  color: #ff7875;
  font-weight: 700;
  cursor: pointer;
}

.review-img-del {
  position: absolute;
  top: 0;
  right: 0;
  width: 20px;
  height: 20px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgb(0 0 0 / 55%);
  color: #fff;
  font-size: 14px;
  line-height: 1;
  cursor: pointer;
  border-radius: 0 0 0 var(--r-md);
}

.review-img-add {
  width: 72px;
  height: 72px;
  border: 1.5px dashed var(--line);
  border-radius: var(--r-md);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 24px;
  color: var(--ink-4);
  cursor: pointer;
}

.review-img-add input {
  display: none;
}

.review-content:focus {
  border-color: var(--brand);
}

.review-count {
  text-align: right;
  font-size: 11px;
  color: var(--ink-4);
  margin-top: 2px;
}

.review-submit {
  width: 100%;
  height: 46px;
  margin-top: 10px;
  border: none;
  border-radius: var(--r-pill);
  background: linear-gradient(135deg, var(--brand), var(--brand-deep));
  color: #fff;
  font-size: 15.5px;
  font-weight: 600;
  cursor: pointer;
}

.review-submit.disabled {
  background: #ddd;
  cursor: not-allowed;
}
</style>
