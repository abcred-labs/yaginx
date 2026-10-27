import {
  Alert,
  AlertDescription,
  AlertTitle,
  Button,
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  RadioGroup,
  RadioGroupItem,
} from "@modra/react";
import React, { useEffect, useState } from "react";

export type IPBlacklistTargetScope = "gateway" | "site";

export const BlacklistScopeDialog: React.FC<{
  open: boolean;
  cidr: string;
  entryLabel: string;
  siteId?: string;
  siteLabel?: string;
  defaultScope?: IPBlacklistTargetScope;
  onClose: () => void;
  onConfirm: (scope: IPBlacklistTargetScope) => Promise<void>;
}> = ({ open, cidr, entryLabel, siteId, siteLabel, defaultScope, onClose, onConfirm }) => {
  const siteAvailable = Boolean(siteId);
  const [scope, setScope] = useState<IPBlacklistTargetScope>(defaultScope === "site" && siteAvailable ? "site" : "gateway");
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (!open) return;
    setScope(defaultScope === "site" && siteAvailable ? "site" : "gateway");
    setSubmitting(false);
  }, [defaultScope, open, siteAvailable]);

  const confirm = async () => {
    setSubmitting(true);
    try {
      await onConfirm(scope);
      onClose();
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={(next) => { if (!next && !submitting) onClose(); }}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>选择黑名单生效范围</DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-4">
          <div className="text-sm">
            封禁对象：<span className="font-semibold">{cidr}</span>（{entryLabel}）
          </div>
          <RadioGroup value={scope} onValueChange={(value) => setScope(value as IPBlacklistTargetScope)} className="flex flex-col gap-3">
            {siteAvailable ? (
              <label className="flex cursor-pointer items-start gap-3 rounded-md border p-3">
                <RadioGroupItem value="site" className="mt-0.5" />
                <span className="flex flex-col gap-1 text-sm">
                  <span className="font-semibold">当前站点黑名单</span>
                  <span className="text-muted-foreground">仅影响 {siteLabel || "当前站点"}。</span>
                </span>
              </label>
            ) : null}
            <label className="flex cursor-pointer items-start gap-3 rounded-md border p-3">
              <RadioGroupItem value="gateway" className="mt-0.5" />
              <span className="flex flex-col gap-1 text-sm">
                <span className="font-semibold">网关黑名单</span>
                <span className="text-muted-foreground">所有站点、所有代理节点及未匹配 Host 的请求都会受到影响。</span>
              </span>
            </label>
          </RadioGroup>
          {scope === "gateway" ? (
            <Alert variant="warning">
              <AlertTitle>高影响范围</AlertTitle>
              <AlertDescription>该规则将在整个网关生效，请确认这不是只针对单个站点的封禁。</AlertDescription>
            </Alert>
          ) : null}
        </div>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={onClose} disabled={submitting}>取消</Button>
          <Button type="button" variant="destructive" onClick={() => void confirm()} loading={submitting}>确认封禁</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};

export default BlacklistScopeDialog;
