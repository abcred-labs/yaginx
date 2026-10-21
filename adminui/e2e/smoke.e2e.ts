import { expect, signIn, test } from './support/api';

const PAGES: Array<[path: string, heading: string]> = [
  ['/', '网关流量概览'],
  ['/website/list', '网站'],
  ['/access-control/ip-blacklist', 'IP 黑名单'],
  ['/monitor/traffic/request-trend', '请求趋势'],
  ['/monitor/requestHistory/list', '请求追踪'],
  ['/resource/hosts', 'Host 资源'],
  ['/systems/runtime', '运行配置'],
  ['/systems/nodes', '节点与发布'],
  ['/systems/acme', 'ACME 订单'],
  ['/systems/notifications', '通知'],
  ['/systems/integrations', 'API 凭据'],
  ['/systems/database', '数据库性能'],
  ['/systems/account', '账号与安全'],
];

test.describe('登录', () => {
  test('校验必填项，成功后进入控制台', async ({ page, api }) => {
    await page.goto('/#/login');
    await page.getByRole('button', { name: '登录' }).click();
    await expect(page.getByText('请输入邮箱地址')).toBeVisible();
    await expect(page.getByText('请输入密码')).toBeVisible();

    await page.getByLabel('邮箱地址').fill('admin@yaginx.local');
    await page.getByLabel('密码').fill('secret');
    await page.getByRole('button', { name: '登录' }).click();

    await expect(page).toHaveURL(/#\/$/);
    await expect(page.getByRole('link', { name: '站点' })).toBeVisible();
    expect(api.lastWrite('/api/account/login')).toMatchObject({ email: 'admin@yaginx.local', password: 'secret' });
  });
});

test.describe('已登录', () => {
  test.beforeEach(async ({ page }) => {
    await signIn(page);
  });

  for (const [path, heading] of PAGES) {
    test(`页面可以打开：${path}`, async ({ page }) => {
      await page.goto(`/#${path}`);
      await expect(page.getByRole('heading', { name: heading, exact: true })).toBeVisible();
    });
  }

  test('侧栏可以收起，并在深浅色之间切换', async ({ page }) => {
    await page.goto('/#/website/list');
    const html = page.locator('html');
    await expect(html).not.toHaveClass(/dark/);
    await page.getByRole('button', { name: '切换到深色模式' }).click();
    await expect(html).toHaveClass(/dark/);
    await page.reload();
    await expect(html).toHaveClass(/dark/);

    const toggle = page.getByRole('button', { name: '切换导航' });
    await expect(toggle).toHaveAttribute('aria-expanded', 'true');
    await toggle.click();
    await expect(toggle).toHaveAttribute('aria-expanded', 'false');
  });

  test('站点列表显示站点，并能进入配置', async ({ page }) => {
    await page.goto('/#/website/list');
    await expect(page.getByRole('cell', { name: 'shop', exact: true })).toBeVisible();
    await expect(page.getByRole('cell', { name: 'blog', exact: true })).toBeVisible();
    await page.getByRole('row', { name: /shop/ }).getByRole('button', { name: '配置' }).click();
    await expect(page).toHaveURL(/#\/website\/5001/);
  });

  test('站点工作区各分区都能打开，未改动时保存的请求体与加载内容一致', async ({ page, api }) => {
    await page.goto('/#/website/5001');
    await expect(page.getByText('已同步')).toBeVisible();

    for (const section of ['hosts', 'routes', 'transforms', 'responses', 'access', 'history']) {
      await page.goto(`/#/website/5001/${section}`);
      await expect(page.getByText('已同步')).toBeVisible();
    }

    await page.goto('/#/website/5001');
    await page.getByRole('button', { name: '保存所有变更' }).click();
    await expect.poll(() => api.lastWrite('/api/website/upsert')).toBeTruthy();

    const saved = api.lastWrite('/api/website/upsert');
    expect(saved).toMatchObject({ id: '5001', name: 'shop', ipAccessMode: 'blacklist', requestHistory: { mode: 'inherit' } });
    expect(saved.hostBindings).toHaveLength(2);
    expect(saved.upstreamPools[0].endpoints).toHaveLength(2);
    // 界面专用的 targetType 不能进入请求体，池规则只带 upstreamPoolId，直连规则只带 destination。
    expect(saved.proxyRules[0]).not.toHaveProperty('targetType');
    expect(saved.proxyRules[0]).toMatchObject({ upstreamPoolId: 'pool-a' });
    expect(saved.proxyRules[0]).not.toHaveProperty('destination');
    expect(saved.proxyRules[1]).toMatchObject({ destination: 'http://10.0.0.9:80', isFallback: true });
  });

  test('修改站点名称后出现未保存提示，保存后带上新名称', async ({ page, api }) => {
    await page.goto('/#/website/5001');
    await page.getByLabel('站点名称').fill('shop2');
    await expect(page.getByText('存在未保存变更')).toBeVisible();
    await page.getByRole('button', { name: '保存所有变更' }).click();
    await expect.poll(() => api.lastWrite('/api/website/upsert')?.name).toBe('shop2');
  });

  test('运行配置：确认后按原结构提交，并回填管理网段', async ({ page, api }) => {
    await page.goto('/#/systems/runtime');
    await page.getByLabel('活动超时').fill('10m');
    await page.getByRole('button', { name: '保存并滚动重启' }).click();
    await page.getByRole('alertdialog').getByRole('button', { name: '保存并滚动重启' }).click();

    await expect.poll(() => api.lastWrite('/api/system_setting/runtime')).toBeTruthy();
    const body = api.lastWrite('/api/system_setting/runtime');
    expect(body).toMatchObject({ expectedRevision: '12', restart: true });
    expect(body.config.proxy.activityTimeout).toBe('10m');
    expect(body.config.management.allowedCIDRs).toEqual(['127.0.0.1/32']);
  });

  test('API 凭据：创建后只显示一次令牌', async ({ page, api }) => {
    await page.goto('/#/systems/integrations');
    await page.getByRole('button', { name: '创建凭据' }).click();
    const dialog = page.getByRole('dialog');
    await dialog.getByRole('button', { name: '创建' }).click();
    await expect(dialog.getByText('请输入名称')).toBeVisible();

    await dialog.getByLabel('名称').fill('测试');
    await dialog.getByLabel('用途').fill('mcp');
    await dialog.getByRole('button', { name: '创建' }).click();

    await expect(page.getByText('请立即复制并保存 Token')).toBeVisible();
    expect(api.lastWrite('/api/system_credentials')).toEqual({ name: '测试', purpose: 'mcp' });
  });

  test('请求追踪：打开详情抽屉', async ({ page }) => {
    await page.goto('/#/monitor/requestHistory/list');
    await page.getByRole('row').nth(1).getByRole('button', { name: '详情' }).click();
    const sheet = page.getByRole('dialog');
    await expect(sheet.getByText('请求诊断详情')).toBeVisible();
    await expect(sheet.getByText('connection refused').first()).toBeVisible();
  });

  test('账号与安全：撤销其它会话需要确认', async ({ page, api }) => {
    api.override('GET /api/account/sessions', () => [
      { id: 's1', userAgent: 'Chrome', clientIp: '10.0.0.8', lastUsedAt: new Date().toISOString(), current: true, remembered: true },
      { id: 's2', userAgent: 'Safari', clientIp: '10.0.0.9', lastUsedAt: new Date().toISOString(), current: false, remembered: false },
    ]);
    await page.goto('/#/systems/account');
    await page.getByRole('row', { name: /Safari/ }).getByRole('button', { name: '退出' }).click();
    await page.getByRole('alertdialog').getByRole('button', { name: '确定' }).click();
    await expect.poll(() => api.lastWrite('/api/account/sessions/s2/revoke')).not.toBeUndefined();
  });
});
