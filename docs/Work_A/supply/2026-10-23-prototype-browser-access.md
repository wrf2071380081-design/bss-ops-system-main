# Lovable 原型浏览器读取问题定位

2026-10-23：使用项目已安装的 Playwright 成功读取最新原型工作台、标准模型库和模型广场，并通过点击侧栏切换页面；截图保存在本机临时目录，没有加入仓库。

## 原因与修正

1. 当前命令执行环境直连 lovable.dev 返回 `ERR_NAME_NOT_RESOLVED`。Playwright 启动必须显式配置 `proxy: { server: 'http://127.0.0.1:12000' }`。
2. 原地址会跳转到 Lovable 分享页，实际产品位于独立的 lovable.app iframe。主页面的 DOMContentLoaded、标题及正文不能代表产品应用已经加载。
3. 过去在等待数秒后就关闭浏览器，未完成的 iframe 请求随关闭返回 `ERR_ABORTED`。本次等待实际应用里的业务控件出现后，预览文档及 CSS/JS 均返回成功，并显示业务数据。
4. 外层 `/permissions` 的 401 没有阻止公开预览加载，不能单凭该响应判断链接无权限。
5. 判断目标 frame 应解析 URL 的 hostname，使用 `hostname.endsWith('.lovable.app')`；不能用完整 URL 的 includes，因为外层分享页 hash 自身就包含内层预览地址。

## 已成功使用的启动方式

项目目录 `model-pricing-web` 中通过 `require('playwright')` 加载本地依赖。使用已安装的 Chrome for Testing：

```js
const browser = await chromium.launch({
  executablePath: 'C:/Users/user/.agent-browser/browsers/chrome-156.0.8078.47/chrome.exe',
  headless: true,
  proxy: { server: 'http://127.0.0.1:12000' },
});
```

打开用户提供的原始分享链接后，在最长约 70 秒内寻找 hostname 以 `.lovable.app` 结束的 frame，并等待其“标准模型库”导航控件出现，再读取正文或点击控件。操作后等待目标业务控件出现，避免只用固定秒数判断已完成加载。终端调用返回 session_id 时继续轮询该进程，不能将它当作命令已完成。

不要输出分享页 hash 或带 `__lovable_token` 的 iframe 完整 URL；排障日志仅记录 hostname、pathname 和状态码。

## 工具边界

agent-browser 0.27.0 的本机原生启动仍返回 `CDP response channel closed`，改用显式 Chrome for Testing 路径也未消除该工具自身错误。本轮已打通 Playwright 路径，后续原型核对可直接使用 Playwright，无需等待 agent-browser 启动问题解决。

本次只验证原型访问及页面导航，没有据此宣称 BSS 项目功能已验收，也没有修改 BSS 业务源码、数据库或部署。
