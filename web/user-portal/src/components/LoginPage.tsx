import { useState } from "react";
import { login } from "../api";
import { errorMessage } from "../lib/utils";
import type { CurrentUser } from "../types";
import type { FormEvent } from "react";

type Props = {
  onAuthenticated: (user: CurrentUser) => void;
};

export function LoginPage({ onAuthenticated }: Props) {
  const [username, setUsername] = useState("admin");
  const [password, setPassword] = useState("admin");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleSubmit(event: FormEvent) {
    event.preventDefault();
    setError(null);
    setSubmitting(true);
    try {
      const payload = await login(username, password);
      onAuthenticated(payload.user);
    } catch (err) {
      setError(errorMessage(err, "登录失败"));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <main className="login-shell">
      <section className="login-hero" aria-label="AgentHub 登录">
        <p className="login-eyebrow">AgentHub</p>
        <h1>登录你的智能工作台</h1>
        <p className="login-copy">以更简洁、安全的方式进入专属 workspace。每个用户的数据与运行环境都会独立隔离。</p>
      </section>

      <form className="login-card" onSubmit={handleSubmit}>
        <div>
          <p className="login-card-kicker">Welcome back</p>
          <h2>用户登录</h2>
          <p className="login-card-copy">内置测试账号：admin / admin</p>
        </div>

        <label className="login-field">
          <span>账号</span>
          <input value={username} onChange={(event) => setUsername(event.target.value)} autoComplete="username" />
        </label>

        <label className="login-field">
          <span>密码</span>
          <input type="password" value={password} onChange={(event) => setPassword(event.target.value)} autoComplete="current-password" />
        </label>

        {error && <p className="login-error" role="alert">{error}</p>}

        <button type="submit" className="login-submit" disabled={submitting}>
          {submitting ? "登录中" : "登录"}
        </button>
      </form>
    </main>
  );
}
