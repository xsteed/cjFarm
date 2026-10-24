import { REVIEW_CONTENT_MAX_LEN, reviewRatingText, reviewTagsOf, sanitizeReviewTags } from '../../core/review';
import { uploadReviewImage } from '../../api/client';
import { toast } from '../../utils/toast';

// 订单评价弹层(仅商家可见的内部反馈):星级 + 快捷标签 + 文字。
// 纯展示交互组件,提交通过 triggerEvent 抛给页面(与 spec-sheet 的受控模式一致);
// 标签联动/清洗口径在 core/review,与 H5 平行对齐。
//
// 实现注意(同 spec-sheet):WXML 表达式不支持函数调用(如 selectedTags.indexOf(x)),
// 标签选中态在 JS 里预计算成 {name, on} 再渲染。

/** 渲染用的标签 chip:on 为选中态。 */
interface TagChip {
  name: string;
  on: boolean;
}

/** 评价图片行:path 为本地临时路径(预览),name 为服务端文件名(上传成功后填)。 */
interface ImgChip {
  path: string;
  name: string;
  uploading: boolean;
  error: boolean;
}

Component({
  properties: {
    visible: { type: Boolean, value: false }
  },

  data: {
    rating: 0,
    tagChips: [] as TagChip[],
    selectedTags: [] as string[],
    content: '',
    contentLen: 0,
    ratingText: '点星星评分',
    submitting: false,
    imgChips: [] as ImgChip[]
  },

  methods: {
    noop() {},

    onClose() {
      this.triggerEvent('close');
    },

    onPickStar(e: WechatMiniprogram.TouchEvent) {
      const n = Number(e.currentTarget.dataset.n) || 0;
      // 切换星级清空已选:标签组跟着换,避免「2 星 + 味道好」的矛盾组合。
      this.setData({
        rating: n,
        tagChips: reviewTagsOf(n).map(name => ({ name, on: false })),
        selectedTags: [],
        ratingText: reviewRatingText(n)
      });
    },

    onToggleTag(e: WechatMiniprogram.TouchEvent) {
      const t = String(e.currentTarget.dataset.tag || '');
      const cur = this.data.selectedTags;
      if (!cur.includes(t) && cur.length >= 6) {
        toast.info('最多选择 6 个标签');
        return;
      }
      const next = cur.includes(t) ? cur.filter(x => x !== t) : [...cur, t];
      this.setData({
        selectedTags: next,
        tagChips: this.data.tagChips.map(c => ({ ...c, on: next.includes(c.name) }))
      });
    },

    onContentInput(e: WechatMiniprogram.Input) {
      const v = String(e.detail.value || '');
      this.setData({ content: v, contentLen: Array.from(v).length });
    },

    onPickImage() {
      const left = 3 - this.data.imgChips.length;
      if (left <= 0) return;
      wx.chooseMedia({
        count: left,
        mediaType: ['image'],
        sizeType: ['compressed'],
        success: res => {
          for (const f of res.tempFiles) {
            this.data.imgChips.push({ path: f.tempFilePath, name: '', uploading: true, error: false });
            this.uploadAt(this.data.imgChips.length - 1);
          }
          this.setData({ imgChips: this.data.imgChips });
        }
      });
    },

    uploadAt(i: number) {
      const img = this.data.imgChips[i];
      if (!img) return;
      uploadReviewImage(img.path, { silent: true })
        .then(res => {
          img.name = (res.url || '').split('/').pop() || '';
          img.uploading = false;
        })
        .catch(() => {
          img.uploading = false;
          img.error = true; // 保留行可重试,不打断其它图
        })
        .then(() => this.setData({ imgChips: this.data.imgChips }));
    },

    onRetryImg(e: WechatMiniprogram.TouchEvent) {
      this.uploadAt(Number(e.currentTarget.dataset.i) || 0);
    },

    onRemoveImg(e: WechatMiniprogram.TouchEvent) {
      const i = Number(e.currentTarget.dataset.i) || 0;
      this.data.imgChips.splice(i, 1);
      this.setData({ imgChips: this.data.imgChips });
    },

    onSubmit() {
      const { rating, submitting, imgChips } = this.data;
      if (!rating || submitting) return;
      if (imgChips.some(c => c.uploading)) {
        toast.info('图片上传中，请稍候');
        return;
      }
      this.setData({ submitting: true });
      this.triggerEvent('submit', {
        rating,
        tags: sanitizeReviewTags(this.data.selectedTags),
        content: Array.from(this.data.content.trim()).slice(0, REVIEW_CONTENT_MAX_LEN).join(''),
        images: imgChips.filter(c => c.name && !c.error).map(c => c.name)
      });
      // 提交中状态复位:页面成功后会关弹层(下次打开是新组件态),失败重开时也不能卡在「提交中」。
      setTimeout(() => this.setData({ submitting: false }), 600);
    }
  }
});
