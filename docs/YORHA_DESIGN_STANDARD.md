# DSPlus - YoRHa 寄叶军用系统界面设计与体验标准 (v1.0)

本文件定义了 DSPlus 项目 GUI 界面重构与迭代中必须强制遵循的 UI/UX 设计标准。所有后续前端组件设计、样式编写与业务交互逻辑，均须严格按照本规范执行，以确保寄叶（YoRHa）终端美学的高度统一性与高还原度。

---

## 1. 核心设计哲学 (Design Philosophy)

* **非饱和度与亚麻色系**：YoRHa 界面模拟了高度结构化、无色彩污染的军用系统。严禁使用高饱和度、刺眼的现代亮色，必须使用淡雅中性、带砂石质感的灰褐底色与墨褐色字符。
* **高对比度高亮**：整个界面仅允许使用单一的高亮亮橘红色 (`#cd5a3f`) 作为状态指示与关键操作焦点，其他辅色需严格控制明暗度。
* **物理精密排版**：
  * 使用大量的非闭合框线、刻度标记、坐标点以及细小文字标称（如 `ROM`、`SYS`、`DUMP`）增加信息精密感。
  * 移除所有现代圆角设计，所有卡片、边框、输入框和下拉框必须为直角边缘。
* **零表情符号规范**：**绝对禁止**使用任何 emoji 表情符号作为按钮、标题或操作状态的图标。图标必须使用统一反色的灰度图片资产（如 `yorha_ui` 本地资产）或极简纯 CSS 几何线段。

---

## 2. 颜色变量系统 (Color Tokens)

在编写 CSS 样式时，必须统一在 `:root` 声明以下主题色值：

| 变量名称 | 色值 | 适用场景 |
| :--- | :--- | :--- |
| `--yorha-bg` | `#d1cdb7` | 全局页面砂石米黄色背景底盘 |
| `--yorha-fg` | `#454138` | 主文字颜色、主刻度边框与基础横线 |
| `--yorha-fg-dim` | `#747266` | 次要文字、标注性数据说明字、次要图标 |
| `--yorha-panel` | `#bab5a1` | 默认状态下按钮的底色、未选中会话项底色 |
| `--yorha-panel-highlight` | `#dcd8c0` | 预选中高亮底色、按钮 Hover 时的取反文字色 |
| `--yorha-accent` | `#cd5a3f` | 寄叶经典高亮亮橘红，适用于警告、突变流量指示、警报横幅、选中划线 |
| `--yorha-accent-dim` | `#9c412b` | 辅助高亮，用于深色高亮状态 |
| `--yorha-dark` | `#2c2b25` | 右侧抽屉、黑色大遮罩面板、CRT 点阵显示器的底色 |

---

## 3. 全局基础排版标准

### 3.1 背景网格线 (Background Grid)
全局 `body` 背景必须叠加微弱的 `0.3rem`（相当于 `4.8px`）微型网格图案，以防大片纯色产生单调感：
```css
body {
  background-color: var(--yorha-bg);
  background-size: 0.3rem 0.3rem;
  background-image: 
    linear-gradient(to right, #ccc8b1 1px, rgba(204,200,177,0) 1px), 
    linear-gradient(to bottom, #ccc8b1 1px, rgba(204,200,177,0) 1px);
}
```

### 3.2 标题体系 (Headings)
* **大标题 (H1)**：字间距须拓宽（`letter-spacing: 0.4rem`），并增加米黄色右下位移投影，模拟老旧印刷感。
* **分级小标题 (H2)**：使用实线上下夹击效果（`border: solid var(--yorha-fg); border-width: 0.1rem 0;`），且需设定内边距。

### 3.3 经典选择点 (Cite Indicator)
在选项或小段落开头需要前置小方块标记时，统一使用以下伪元素配置：
```css
cite:before {
  content: '';
  position: absolute;
  width: 0.6rem;
  height: 0.6rem;
  background-color: var(--yorha-fg);
  left: 0;
  top: 0.3em;
}
```

---

## 4. 交互组件开发规范

### 4.1 寄叶经典按钮 (YoRHa Hover Physics Button)
按钮必须支持寄叶标志性的“从左向右拉满填充并上下出线”的动效。样式必须完整重现以下伪元素物理动画：

* **基础定义**：
  ```css
  .button {
    position: relative;
    z-index: 1;
    background-color: var(--yorha-panel);
    transition: color 0.2s, background-color 0.2s, box-shadow 0.2s;
  }
  ```
* **上下出线与左侧充填**：
  ```css
  /* 悬停时上下出现的 0.1rem 直角边框 */
  .button:hover:before {
    content: '';
    position: absolute;
    top: -0.2rem;
    bottom: -0.2rem;
    left: 0;
    right: 0;
    border: solid var(--yorha-fg);
    border-width: 0.1rem 0;
  }
  /* 悬停时从左往右拉满的深灰色背景 */
  .button:after {
    content: '';
    position: absolute;
    top: 0; bottom: 0; left: 0;
    width: 0;
    background-color: var(--yorha-fg);
    z-index: -1;
    transition: all 0.2s ease-out;
  }
  .button:hover:after {
    width: 100%;
  }
  .button:hover {
    background-color: transparent;
    color: var(--yorha-panel-highlight);
    box-shadow: 0.2em 0.2em 0.1em 0 var(--yorha-panel);
  }
  ```

### 4.2 卡片组件 (Figure Layout)
用于仪表盘数据展示的指标卡片统一采用原生的 `<figure>` 与 `<figcaption>` 块结构：
* `figcaption`：充当卡片表头，背景为重色 `var(--yorha-fg)`，文字为偏亮米色 `var(--yorha-panel)`，英文字母强制大写。
* `figure` 盒：四周必须带有 `1px solid var(--yorha-panel)` 实边线，直角收边。

### 4.3 列表与表格 (Modern Tables)
* **表格布局**：禁止包含任何斑马纹底色。边框为四周闭合的 `0.15rem solid var(--yorha-fg)`，每行底边辅以 `1px solid var(--yorha-panel)` 细实线。
* **选中高亮行 (.selected-row)**：激活的行不采用现代半透明高亮，而是整行填充为 `var(--yorha-fg)`（重灰黑色），行内所有文本反白为 `var(--yorha-panel-highlight)`。

### 4.4 对话气泡 (YoRHa Message Bubble)
聊天诊断对话气泡严禁使用圆角矩形，必须完全直角，并使用双横线夹击形式呈现：
```css
.yorha-msg-bubble {
  border: 0.1rem solid var(--yorha-fg);
  padding: 1rem;
  position: relative;
}
/* 用户输入气泡：右边框加粗呈现亮橘红色高亮条 */
.yorha-msg-bubble.user {
  border-right: 0.4rem solid var(--yorha-accent);
}
/* AI响应气泡：左边框加粗呈现深灰色高亮条 */
.yorha-msg-bubble.assistant {
  border-left: 0.4rem solid var(--yorha-fg);
}
```

### 4.5 滑块开关 (YoRHa Toggle Switch)
开关按钮由外围的扁平直角矩形和中心代表开启/关闭的矩形方块构成。为确保不同缩放与平台下的对齐精度，禁止使用 `bottom` 或 `top` 具体像素值做方块对齐，必须统一采用 CSS `translateY` 绝对垂直居中定位：
```css
.yorha-slider:before {
  content: "";
  position: absolute;
  height: 12px;
  width: 16px;
  left: 3px;
  top: 50%;
  transform: translateY(-50%); /* 1. 默认居中对齐 */
  background-color: var(--yorha-fg);
  transition: transform 0.15s;
}
/* 开启状态：横向做平滑的 translateY 继承滑动 */
.yorha-switch input:checked + .yorha-slider:before {
  transform: translate(20px, -50%); /* 2. 滑动且保持绝对垂直居中 */
  background-color: var(--yorha-accent);
}
```

### 4.6 统一 SVG 图标规范 (Universal SVG Icons)
为避免在不同环境下引入外部图片资产失败的问题，且为保持高精度的线条美感，所有 Tab 导航或按钮内部图标统一改用极简 SVG 矢量图，必须遵循以下标准：
* **无填充描边设定**：SVG 图标必须使用 `fill="none"`、`stroke="currentColor"` 和 `stroke-width="2.5"`（或 `2.0`），确保图标颜色能够直接通过 CSS 自适应按钮文字的状态（普通状态为深灰褐色，Hover/Active 状态为米黄色），无需应用复杂的 CSS `filter`。
* **物理尺寸约束**：统一设定宽高为 `14px`，并配合 `vertical-align: middle;` 保持与文字的垂直居中对齐：
  ```css
  .button .tab-icon {
    width: 14px;
    height: 14px;
    vertical-align: middle;
    margin-right: 8px;
    margin-top: -2px;
  }
  ```

### 4.7 模拟下拉选择组件 (YoRHa Select Component)
原生下拉组件样式难以定制，系统采用 CSS/JS 模拟的自定义下拉菜单：
* **指示箭头背景**：下拉触发器使用嵌入式的 SVG DataURI 作为上下箭头的背景。当面板展开时，触发器背景色取反，同时切换为与之形成高对比度的反色 DataURI 图标：
  ```css
  .yorha-select-trigger {
    padding: 0.5rem 2rem 0.5rem 1rem;
    background-color: #dbd7c1;
    color: #444138;
    background-image: url("data:image/svg+xml;..."); /* 默认深色双箭头 */
    background-repeat: no-repeat;
    background-position: right 0.5rem center;
    background-size: 0.6rem;
  }
  .yorha-select-trigger:hover,
  .yorha-select-wrapper.open .yorha-select-trigger {
    background-color: #444138;
    color: #dbd7c1;
    background-image: url("data:image/svg+xml;...") !important; /* 取反浅色双箭头 */
  }
  ```
* **直角悬浮面板**：选项面板 `.yorha-select-options` 必须采用 `position: absolute;` 悬浮，设置强烈的直角实线边框，并附加经典的硬投影效果：
  ```css
  .yorha-select-options {
    background-color: #dbd7c1;
    border: 1px solid #444138;
    box-shadow: 0.3rem 0.3rem 0 rgba(0, 0, 0, 0.15);
  }
  ```

### 4.8 数字微调步进器 (YoRHa Number Stepper)
数字微调组件用于进行精确的配置数值修改：
* **扁平直角整合**：外框 `.yorha-number-stepper` 的高度固定为 `2.2rem`，去掉所有内外边框。
* **隐去原生控件**：必须完全隐去浏览器默认的上下微调滚轮（通过 Webkit 伪元素 `::-webkit-outer-spin-button` 和 Firefox 的 `-moz-appearance: textfield` 等）。
* **加减按钮定制**：左右两个控制按钮 `.stepper-btn` 必须显式禁用 `::before` 与 `::after` 伪元素的 hover 特效（设置为 `display: none !important; content: none !important;`），使其退化为扁平直角反色按钮，防止复杂的缩放导致框线破裂：
  ```css
  .yorha-number-stepper .stepper-btn:before,
  .yorha-number-stepper .stepper-btn:after {
    display: none !important;
    content: none !important;
  }
  ```

### 4.9 警报系统横幅 (YoRHa Alert Banner)
用于显示异步的系统通知：
* **直角与经典高亮条**：警报横幅固定悬浮于页面左下角，背景采用重深色背景，左侧带有极具标志性的 `0.4rem` 宽亮橘红边框（`border-left: 0.4rem solid var(--yorha-accent)`）。
* **缓动过渡状态**：通过 JS 动态添加/移除 `.show` 类来控制通知的滑入与消失动画：
  ```css
  .yorha-alert-banner {
    position: fixed;
    bottom: 2rem;
    left: 3rem;
    opacity: 0;
    transform: translateY(1rem);
    transition: all 0.2s ease-out;
  }
  .yorha-alert-banner.show {
    opacity: 1;
    transform: translateY(0);
  }
  ```

### 4.10 折叠详情面板 (Detail Collapse Panel)
针对高密度的调试信息或详细 Trace 数据，使用直角卡片折叠面板：
* **小三角形指示器**：标题 `.detail-label` 左侧放置小三角形字符，折叠时使用 CSS `transform: rotate(-90deg);` 实现向下与向左的指向变换。
* **内容物微缩排版**：内容区 `.detail-content` 须强制使用等宽字体 `Consolas`，字号微调至 `0.8rem`，背景为微弱半透明黑色底，确保代码和 JSON 数据可读性。
* **紧凑型复制按钮**：在折叠卡片右上角放置的 `.copy-btn` 需使用绝对定位（`position: absolute; right: 0.5rem; top: 0.45rem;`），并且同样禁用 before/after 的拉伸线段动画，简化为极简直边线框，Hover 时取反为亮橘红色底与米黄色字：
  ```css
  .detail-section .copy-btn {
    position: absolute;
    right: 0.5rem;
    top: 0.45rem;
    background-color: transparent !important;
    border: 1px solid rgba(220, 216, 192, 0.4) !important;
  }
  .detail-section .copy-btn:hover {
    background-color: var(--yorha-accent) !important;
    border-color: var(--yorha-accent) !important;
    color: var(--yorha-panel-highlight) !important;
  }
  .detail-section .copy-btn:before,
  .detail-section .copy-btn:after {
    content: none !important;
    display: none !important;
  }
  ```

---

## 5. CRT 复古屏幕滤镜规范 (已废弃/注释禁用)

> [!WARNING]
> **设计变更说明**：在 v2.0 及后续版本中，双图层像素网点背景滤镜已默认被**注释禁用**。主要考量包括：
> 1. **文本清晰度与可读性**：网点层叠易导致高密度调试文本产生摩尔纹，干扰对关键 API 日志信息的辨识。
> 2. **性能与渲染开销**：在低配客户端或高分辨率显示器下渲染复杂的 CSS 渐变点阵会导致 GPU/CPU 渲染负荷增加，产生滚动卡顿。
>
> 故系统建议在生产环境下默认关闭该特效。以下规范和代码实现仅保留用于设计备忘与参考。

### 5.1 双图层像素网点 (Dual-layer Grid Dots) - 默认注释
使用两组不同尺度周期的 `linear-gradient` 网格线层叠相交，形成每隔 4 个点出现一个小点的大矩形点阵暗影，表现阴极射线管点阵（在 CSS 中须以多行注释包裹）：
```css
.yorha-drawer-overlay {
  background-color: var(--yorha-dark);
  background-size: 5px 5px, 5px 5px, 20px 20px, 20px 20px;
  /* 默认注释禁用：
  background-image:
    linear-gradient(to right, rgba(0, 0, 0, 0.28) 1px, transparent 1px),
    linear-gradient(to bottom, rgba(0, 0, 0, 0.28) 1px, transparent 1px),
    linear-gradient(to right, rgba(0, 0, 0, 0.20) 2px, transparent 2px),
    linear-gradient(to bottom, rgba(0, 0, 0, 0.20) 2px, transparent 2px);
  */
}
```

### 5.2 扫描线与色偏滤镜 (Scanline & Chromatic Aberration)
在深色面板的伪元素上叠加物理扫描线与轻微的 RGB 色散层：
```css
.yorha-drawer-overlay::before {
  content: " ";
  display: block;
  position: absolute;
  top: 0; left: 0; bottom: 0; right: 0;
  background: 
    linear-gradient(rgba(18, 16, 16, 0) 50%, rgba(0, 0, 0, 0.15) 50%), 
    linear-gradient(90deg, rgba(255, 0, 0, 0.03), rgba(0, 255, 0, 0.01), rgba(0, 0, 255, 0.03));
  background-size: 100% 4px, 6px 100%;
  z-index: 5;
  pointer-events: none; /* 确保鼠标事件可以穿透 */
  opacity: 0.7;
}
```
* **开发提示**：为防色偏背景滤镜导致内部文本或操作按钮模糊，抽屉内部的内容容器（如 `.yorha-drawer-header`、`.yorha-drawer-body`）的 `z-index` 必须被显式指定为 `10` 以上，以覆盖在扫描滤镜层之上。
