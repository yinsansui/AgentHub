import { ArrowUp, ChevronDown, Plus } from "lucide-react";
import { useCallback, useEffect, useId, useMemo, useRef, useState } from "react";
import type { FormEvent } from "react";
import type { LLMModel, WorkbenchState } from "../types";
import { MessageBubble } from "./MessageBubble";

type Props = {
  workbench: WorkbenchState;
  enabledModels: LLMModel[];
  defaultModelId?: string;
  messageDraft: string;
  setMessageDraft: (v: string) => void;
  selectedModelId: string;
  setSelectedModelId: (v: string) => void;
  onSubmit: (e: FormEvent) => void;
  onStop: () => void;
};

function ModelDropdown({
  value,
  onChange,
  options,
  disabled,
  defaultModelId,
  lockedModelId,
}: {
  value: string;
  onChange: (v: string) => void;
  options: LLMModel[];
  disabled: boolean;
  defaultModelId?: string;
  lockedModelId?: string;
}) {
  const [open, setOpen] = useState(false);
  const [activeIndex, setActiveIndex] = useState(0);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const id = useId();
  const listId = `${id}-list`;

  const allOptions = useMemo(
    () => [
      ...(defaultModelId ? [{ label: `默认：${defaultModelId}`, value: "" }] : []),
      ...options.map((m) => ({ label: m.modelId, value: m.modelId })),
    ],
    [options, defaultModelId]
  );

  const selectedLabel = lockedModelId
    ? lockedModelId
    : allOptions.find((o) => o.value === value)?.label ?? (defaultModelId ? `默认：${defaultModelId}` : "选择模型");

  const handleOpen = useCallback(() => {
    if (disabled) return;
    const idx = allOptions.findIndex((o) => o.value === value);
    setActiveIndex(idx >= 0 ? idx : 0);
    setOpen(true);
  }, [disabled, allOptions, value]);

  const handleClose = useCallback((restoreFocus = true) => {
    setOpen(false);
    if (restoreFocus) {
      triggerRef.current?.focus();
    }
  }, []);

  const selectOption = useCallback(
    (index: number) => {
      if (disabled) return;
      const opt = allOptions[index];
      if (opt && opt.value !== value) {
        onChange(opt.value);
      }
      setOpen(false);
      triggerRef.current?.focus();
    },
    [allOptions, disabled, onChange, value]
  );

  useEffect(() => {
    if (disabled) {
      setOpen(false);
    }
  }, [disabled]);

  useEffect(() => {
    if (!open) return;
    const onDocClick = (e: MouseEvent) => {
      const target = e.target as Node;
      if (
        listRef.current?.contains(target) ||
        triggerRef.current?.contains(target)
      ) {
        return;
      }
      handleClose(false);
    };
    document.addEventListener("mousedown", onDocClick);
    return () => document.removeEventListener("mousedown", onDocClick);
  }, [open, handleClose]);

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      switch (e.key) {
        case "Escape":
          e.preventDefault();
          handleClose();
          break;
        case "ArrowDown":
          e.preventDefault();
          setActiveIndex((i) => Math.min(i + 1, allOptions.length - 1));
          break;
        case "ArrowUp":
          e.preventDefault();
          setActiveIndex((i) => Math.max(i - 1, 0));
          break;
        case "Enter":
        case " ":
          e.preventDefault();
          selectOption(activeIndex);
          break;
        case "Tab":
          handleClose(false);
          break;
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [open, activeIndex, allOptions.length, handleClose, selectOption]);

  useEffect(() => {
    if (open) {
      listRef.current?.querySelector<HTMLElement>(`[data-index="${activeIndex}"]`)?.focus();
    }
  }, [open, activeIndex]);

  return (
    <div className="model-dropdown">
      <button
        ref={triggerRef}
        type="button"
        className={`model-dropdown-trigger${open ? " open" : ""}`}
        onClick={() => (open ? handleClose() : handleOpen())}
        onKeyDown={(e) => {
          if (["ArrowDown", "ArrowUp"].includes(e.key)) {
            e.preventDefault();
            handleOpen();
          }
        }}
        disabled={disabled}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={listId}
        id={`${id}-trigger`}
      >
        <span className="model-dropdown-label">{selectedLabel}</span>
        <ChevronDown size={14} />
      </button>
      {open && (
        <div
          ref={listRef}
          id={listId}
          className="model-dropdown-list"
          role="listbox"
          aria-labelledby={`${id}-trigger`}
          aria-activedescendant={`${id}-opt-${activeIndex}`}
        >
          {allOptions.map((opt, i) => (
            <div
              key={opt.value}
              id={`${id}-opt-${i}`}
              data-index={i}
              className={`model-dropdown-option${i === activeIndex ? " active" : ""}${opt.value === value ? " selected" : ""}`}
              role="option"
              aria-selected={opt.value === value}
              tabIndex={-1}
              onMouseEnter={() => setActiveIndex(i)}
              onClick={() => selectOption(i)}
            >
              {opt.label}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

export function ChatPanel({
  workbench, enabledModels, defaultModelId,
  messageDraft, setMessageDraft,
  selectedModelId, setSelectedModelId,
  onSubmit
}: Props) {
  const hasModel = workbench.sessionId
    ? Boolean(workbench.modelId)
    : Boolean(defaultModelId || selectedModelId);
  const canSend = messageDraft.trim() !== "" && !workbench.activeRun && hasModel;
  const modelHint = enabledModels.length === 0
    ? "请先在设置中配置并启用至少一个模型"
    : "请选择一个模型，或在设置中设为默认模型";

  return (
    <>
      {workbench.systemError && <div className="notice error">{workbench.systemError}</div>}

      <div className="conversation-mask">
        <div className="h-full overflow-auto px-5 py-[30px] pb-9 flex flex-col gap-2.5">
          {workbench.messages.map((message) => (
            <MessageBubble key={message.messageId} message={message} />
          ))}
          {workbench.messages.length === 0 && (
            <div className="w-[min(640px,100%)] min-h-[260px] mx-auto grid place-items-center content-center gap-2 text-center text-apple-fg-50">
              <h3 className="text-apple-fg text-lg">有什么我可以帮你的？</h3>
            </div>
          )}
        </div>
      </div>

      <form className="mx-auto w-[min(840px,calc(100%-40px))] py-3 pb-4 max-[700px]:w-[calc(100%-24px)]" onSubmit={onSubmit}>
        <div className="bg-apple-panel shadow-apple-card rounded-[18px] px-3.5 pt-3.5 pb-2.5 flex flex-col gap-2.5">
          <textarea
            className="min-h-[72px] max-h-[200px] rounded-none p-0 bg-transparent shadow-none resize-none border-none outline-none text-[15px] leading-normal"
            value={messageDraft}
            onChange={(e) => setMessageDraft(e.target.value)}
            placeholder="给 AgentHub 发送消息…"
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey) {
                e.preventDefault();
                if (canSend) e.currentTarget.form?.requestSubmit();
              }
            }}
          />
          <div className="flex items-center justify-between gap-2">
            <div className="flex items-center gap-1">
              <button type="button" className="w-[30px] h-[30px] min-h-[30px] p-0 rounded-lg bg-transparent shadow-none text-apple-fg-50 hover:bg-apple-fg-5 hover:text-apple-fg" aria-label="添加"><Plus size={16} /></button>
              <ModelDropdown
                value={selectedModelId}
                onChange={setSelectedModelId}
                options={enabledModels}
                disabled={Boolean(workbench.sessionId)}
                defaultModelId={defaultModelId}
                lockedModelId={workbench.sessionId ? workbench.modelId : undefined}
              />
            </div>
            <button type="submit" className="w-8 h-8 min-h-8 p-0 rounded-full bg-apple-accent shadow-none text-white transition-opacity duration-[120ms] ease-out hover:bg-apple-accent-hover hover:text-white disabled:bg-black/15 disabled:text-black/35 disabled:opacity-100" disabled={!canSend} aria-label="发送">
              <ArrowUp size={16} />
            </button>
          </div>
          {!hasModel && !workbench.sessionId && (
            <p className="text-[12px] text-apple-fg-50 -mt-1">{modelHint}</p>
          )}
        </div>
      </form>
    </>
  );
}
