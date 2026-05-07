import { test, expect, type Page } from "@playwright/test";

test.describe("Workspace CRUD", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
    await page.fill("input[autocomplete='username']", "admin");
    await page.fill("input[autocomplete='current-password']", "admin");
    await page.click("button:has-text('登录')");
    await page.waitForSelector("text=新 Session", { timeout: 10000 });
  });

  function openWorkspaceMenu(page: Page) {
    return page.locator("[data-testid='workspace-menu-trigger']").click();
  }

  async function createWorkspace(page: Page, name: string) {
    await openWorkspaceMenu(page);
    await page.click("button:has-text('新建 Workspace')");
    await expect(page.locator("text=新建 Workspace")).toBeVisible();
    await page.fill("input[placeholder='Workspace 名称']", name);
    await page.click("button:has-text('创建')");
    await page.waitForTimeout(1000);
    await expect(page.locator("text=新建 Workspace")).not.toBeVisible();
    return new URL(page.url()).searchParams.get("workspaceId");
  }

  async function openWorkspaceSettings(page: Page) {
    await page.click("button:has-text('设置')");
    await page.waitForTimeout(500);
    await page.click("button:has-text('Workspace')");
    await page.waitForTimeout(500);
  }

  test("应该显示 workspace 下拉菜单并包含新建按钮", async ({ page }) => {
    await openWorkspaceMenu(page);
    await page.waitForTimeout(500);
    await expect(page.locator("button:has-text('新建 Workspace')")).toBeVisible();
    await page.keyboard.press("Escape");
  });

  test("应该能通过弹窗创建 workspace", async ({ page }) => {
    const uniqueName = `E2E-${Date.now()}`;
    const createdId = await createWorkspace(page, uniqueName);
    expect(createdId).toBeTruthy();
    expect(createdId).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i);
  });

  test("Settings 只显示并管理当前 workspace", async ({ page }) => {
    const firstName = `CURRENT-${Date.now()}`;
    const secondName = `OTHER-${Date.now()}`;

    await createWorkspace(page, firstName);
    await createWorkspace(page, secondName);
    await openWorkspaceMenu(page);
    await page.locator("button", { hasText: firstName }).click();
    await page.waitForTimeout(500);

    await openWorkspaceSettings(page);
    await expect(page.locator("input[placeholder='Workspace 名称']")).toHaveValue(firstName);
    await expect(page.locator("text=Settings 只管理当前选中的 workspace")).not.toBeVisible();
    await expect(page.locator("text=" + secondName)).not.toBeVisible();
  });

  test("应该在 Settings 中重命名和删除当前 workspace", async ({ page }) => {
    const uniqueName = `E2E-${Date.now()}`;
    const renamedName = `RENAMED-${Date.now()}`;

    await createWorkspace(page, uniqueName);
    await openWorkspaceSettings(page);
    await page.fill("input[placeholder='Workspace 名称']", renamedName);
    await page.click("button:has-text('保存')");
    await page.waitForTimeout(1000);

    await page.click("button:has-text('返回工作台')");
    await page.waitForTimeout(500);
    await expect(page.locator("[data-testid='workspace-menu-trigger']", { hasText: renamedName })).toBeVisible();

    await openWorkspaceSettings(page);
    await page.locator("button[aria-label='删除 Workspace']").click();
    await expect(page.getByRole("heading", { name: "删除 Workspace" })).toBeVisible();
    await expect(page.locator("text=请输入 workspace 名称以确认删除")).toBeVisible();
    await page.fill("input[placeholder='输入 workspace 名称']", renamedName);
    await page.locator("button[aria-label='确认删除 Workspace']").click();
    await page.waitForTimeout(1000);

    await expect(page.locator("[data-testid='workspace-menu-trigger']", { hasText: renamedName })).not.toBeVisible();
  });

  test("删除当前 workspace 后应该自动切换到其他剩余 workspace", async ({ page }) => {
    const deletedName = `DELETE-${Date.now()}`;

    await createWorkspace(page, `REMAIN-${Date.now()}`);
    const deletedId = await createWorkspace(page, deletedName);
    await openWorkspaceSettings(page);
    await page.locator("button[aria-label='删除 Workspace']").click();
    await page.fill("input[placeholder='输入 workspace 名称']", deletedName);
    await page.locator("button[aria-label='确认删除 Workspace']").click();
    await page.waitForTimeout(1000);

    expect(new URL(page.url()).searchParams.get("workspaceId")).not.toBe(deletedId);
    await expect(page.locator("[data-testid='workspace-menu-trigger']", { hasText: deletedName })).not.toBeVisible();
  });

  test("删除时输入错误名称应该阻止删除", async ({ page }) => {
    const uniqueName = `E2E-${Date.now()}`;

    await createWorkspace(page, uniqueName);
    await openWorkspaceSettings(page);
    await page.locator("button[aria-label='删除 Workspace']").click();
    await page.fill("input[placeholder='输入 workspace 名称']", "错误的名称");
    await page.locator("button[aria-label='确认删除 Workspace']").click();
    await expect(page.locator("text=输入的名称与 workspace 名称不一致")).toBeVisible();
  });
});
