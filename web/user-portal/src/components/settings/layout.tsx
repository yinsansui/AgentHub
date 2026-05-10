import type { ReactNode } from "react";

export function PageShell(props: { actions?: ReactNode; children: ReactNode }) {
  return (
    <section className="settings-page">
      {props.actions && <div className="settings-page-actions">{props.actions}</div>}
      <div className="settings-page-body">{props.children}</div>
    </section>
  );
}

export function SectionCard(props: { title: string; description?: string; actions?: ReactNode; children: ReactNode }) {
  return (
    <section className="settings-card">
      <div className="settings-card-header">
        <div>
          <h2>{props.title}</h2>
          {props.description && <p>{props.description}</p>}
        </div>
        {props.actions && <div className="settings-card-actions">{props.actions}</div>}
      </div>
      {props.children}
    </section>
  );
}
