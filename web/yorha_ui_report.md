# YoRHa (尼尔: 机械纪元) 主题设计原型报告

已根据您的设想，为 DSPlus 编写了一套极具寄叶部队 (YoRHa) 风格的静态交互页面：[demo_yorha.html](file:///f:/AI%20code/DSPlus/web/demo_yorha.html)。

该设计严格还原了《尼尔：机械纪元》游戏内标志性的扁平化军用 OS 界面。以下为该风格的技术落地与排版效果说明：

## 核心美学规范

### 1. 配色方案
* **浅色砂石底色**：背景采用淡灰暖褐色的高雅米黄色 (`#cdcbbf`)，既保证了视觉舒适度，又具备极高的军事终端辨识度。
* **墨褐色前景字**：文字与基础线条采用重墨褐色 (`#3c3b33`)，确保了在中性底色上的极致对比。
* **高亮橙红色**：所有选中状态、警报信息和高亮元素均使用寄叶经典的温暖橙红 (`#cd5a3f`) 作为点缀。

### 2. 界面细节与辅助元素
* **防扫描线滤镜**：全屏注入微弱的水平扫描线纹理，模拟军用老旧显像管终端的数字脉冲感。
* **非闭合细边框**：所有面板和卡片均采用断裂细线边框，四角配有标志性的精微十字定位坐标和装饰点。
* **零 Emoji 约定**：彻底移除所有按钮和标题中的 emoji 字符，取而代之的是纯粹的矢量线段、等宽数据和方块指示器。

---

## 交互与页面组件排版效果

下面是使用浏览器代理对该 YoRHa 原型页面执行的交互路径所录制的演示视频：

![YoRHa UI 动态演示视频](file:///C:/Users/thehuan/.gemini/antigravity-ide/brain/3cba13bd-0de8-447a-a7ee-1f0813880ef8/yorha_ui_demo_1781064747511.webp)

同时，您可以滑动查看不同界面的静态截图效果：

````carousel
![Dashboard 主界面 (YoRHa 风格)](file:///C:/Users/thehuan/.gemini/antigravity-ide/brain/3cba13bd-0de8-447a-a7ee-1f0813880ef8/yorha_drawer_closed_1781064866614.png)
<!-- slide -->
![请求详情侧边抽屉展示](file:///C:/Users/thehuan/.gemini/antigravity-ide/brain/3cba13bd-0de8-447a-a7ee-1f0813880ef8/yorha_drawer_open_1781064836855.png)
<!-- slide -->
![会话诊断分析页（极简边框与对话气泡）](file:///C:/Users/thehuan/.gemini/antigravity-ide/brain/3cba13bd-0de8-447a-a7ee-1f0813880ef8/yorha_diagnosis_page_1781064881103.png)
<!-- slide -->
![配置设置卡片与切换开关](file:///C:/Users/thehuan/.gemini/antigravity-ide/brain/3cba13bd-0de8-447a-a7ee-1f0813880ef8/yorha_config_page_1781064904397.png)
<!-- slide -->
![底层警报弹窗触发效果](file:///C:/Users/thehuan/.gemini/antigravity-ide/brain/3cba13bd-0de8-447a-a7ee-1f0813880ef8/yorha_config_alert_1781064914446.png)
````

### 3. 排版细节设计
* **Dashboard (仪表盘)**：
  * **数据卡片**：使用带有定位角的轻盈底色块，配以细微小字版本的 `ROM` / `SYS` 编号，突出系统感。
  * **延时栅格图**：响应延迟不再单调显示数字，而是配以独特的 `yorha-grid-bar` 格栅小竖条。
  * **详情弹窗**：通过右侧滑出的黑灰撞色遮罩面板（`yorha-drawer`）展示 JSON 数据块，排版利落。
* **Diagnosis (诊断分析)**：
  * **双栏历史**：左侧历史会话列表选中时会被深色完全充填，文字反白。
  * **无底色线段气泡**：聊天对话放弃现代圆角气泡，转为仅带边缘单边条装饰的经典框，突出对话原始纯净度。
  * **芯片折叠面板**：推理思考过程封装进特有的 `YoRHa Auto-Align` 芯片框，并以黑方块 `■` 作为状态标记。
* **System Config (系统配置)**：
  * **芯片式卡片**：各项参数配置分类呈现在配有定位标志的芯片式选项板中。
  * **滑块微动效**：复选开关在点击时，小斜线滑块做无过渡延迟的清脆横向位移，配合低对比度呼吸色。
