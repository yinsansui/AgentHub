import { ArrowUp, Plus } from "lucide-react";
import type { FormEvent } from "react";
import type { LLMModel, WorkbenchState } from "../types";
import type { Notice } from "../hooks/useWorkspace";
import { MessageBubble } from "./MessageBubble";

type Props = {
  workbench: WorkbenchState;
  notice: Notice | null;
  enabledModels: LLMModel[];
  messageDraft: string;
  setMessageDraft: (v: string) => void;
  selectedModelId: string;
  setSelectedModelId: (v: string) => void;
  onSubmit: (e: FormEvent) => void;
  onStop: () => void;
};

export function ChatPanel({
  workbench, notice, enabledModels,
  messageDraft, setMessageDraft,
  selectedModelId, setSelectedModelId,
  onSubmit
}: Props) {
  const canSend = messageDraft.trim() !== "" && !workbench.activeRun;

  return (
    <>
      {notice && <div className={`notice ${notice.tone}`}>{notice.text}</div>}
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
            placeholder="Message AgentHub..."
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey) {
                e.preventDefault();
                if (canSend) e.currentTarget.form?.requestSubmit();
              }
            }}
          />
          <div className="flex items-center justify-between gap-2">
            <div className="flex items-center gap-1">
              <button type="button" className="w-[30px] h-[30px] min-h-[30px] p-0 rounded-lg bg-transparent shadow-none text-apple-fg-50 hover:bg-apple-fg-5 hover:text-apple-fg" aria-label="Add"><Plus size={16} /></button>
              <select
                className="w-auto h-[30px] px-2 rounded-lg shadow-none bg-transparent text-[13px] text-apple-fg-50 cursor-pointer"
                value={selectedModelId}
                onChange={(e) => setSelectedModelId(e.target.value)}
                disabled={Boolean(workbench.sessionId)}
              >
                <option value="">Default model</option>
                {enabledModels.map((m) => <option key={m.modelId} value={m.modelId}>{m.modelId}</option>)}
              </select>
            </div>
            <button type="submit" className="w-8 h-8 min-h-8 p-0 rounded-full bg-apple-accent shadow-none text-white transition-opacity duration-[120ms] ease-out hover:bg-apple-accent-hover hover:text-white disabled:bg-black/15 disabled:text-black/35 disabled:opacity-100" disabled={!canSend} aria-label="Send">
              <ArrowUp size={16} />
            </button>
          </div>
        </div>
      </form>
    </>
  );
}
