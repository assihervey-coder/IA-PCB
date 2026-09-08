"use client";

import { Button, type DataAttributes } from "@/app/components/buttons/Button";
import { Modal } from "./Modal";

export interface ConfirmDialogProps extends DataAttributes {
  title: string;
  message: string;
  confirmLabel?: string;
  cancelLabel?: string;
  onConfirm: () => void;
  onCancel: () => void;
}

export function ConfirmDialog({
  title,
  message,
  confirmLabel = "Supprimer",
  cancelLabel = "Annuler",
  onConfirm,
  onCancel,
  ...rest
}: ConfirmDialogProps) {
  return (
    <div {...rest}>
      <Modal title={title} onClose={onCancel} maxWidth={420}>
        <p className="confirm-message">{message}</p>
        <div className="modal-footer" style={{ borderTop: "none", padding: "8px 0 0" }}>
          <Button variant="ghost" onClick={onCancel}>
            {cancelLabel}
          </Button>
          <Button variant="danger" onClick={onConfirm}>
            {confirmLabel}
          </Button>
        </div>
      </Modal>
    </div>
  );
}
