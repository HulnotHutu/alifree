import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import type { InferEnvelope } from "../types";

// 推理请求统一走 useMutation：pending / 结果 / 错误全部由 Query 管理，
// 各 Tab 不再手写 loading/out 状态对。前端校验失败用 fail() 注入同一个
// 信封；后端返回 ok:false 也归一成 Error，渲染逻辑只认 out 一个来源。
export function useInfer<A extends unknown[]>(fn: (...args: A) => Promise<InferEnvelope>) {
  const [clientErr, setClientErr] = useState<string | null>(null);
  const mut = useMutation({
    mutationFn: async (variables: A) => {
      const env = await fn(...variables);
      if (!env.ok) throw new Error(env.error || "请求失败");
      return env;
    },
  });

  const out: InferEnvelope | null = clientErr
    ? { ok: false, error: clientErr }
    : mut.isError
      ? { ok: false, error: (mut.error as Error).message }
      : mut.data ?? null;

  return {
    out,
    loading: mut.isPending,
    send: (...args: A) => {
      setClientErr(null);
      mut.mutate(args);
    },
    fail: (msg: string) => setClientErr(msg),
  };
}
