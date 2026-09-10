import { Space, Button, theme } from "antd";
import { SeverityTag } from "./SeverityTag";
import { StatusTag } from "./StatusTag";
import { formatDateTime } from "../utils/time";
import { getAlertActions, type Alert } from "./AlertTable";

export interface AlertCardProps {
  alert: Alert;
  onAck: (id: string) => void;
  onResolve: (id: string) => void;
  onMarkFP?: (id: string, isFP: boolean) => void;
  // D-3：告警一键建单（undefined 时不渲染该按钮）
  onCreateTicket?: (id: string) => void;
}

// M13：移动端单条告警卡片（替代表格在 xs 断点横向溢出）。操作按钮走 getAlertActions 与桌面端同一决策。
export function AlertCard({ alert, onAck, onResolve, onMarkFP, onCreateTicket }: AlertCardProps) {
  const { token } = theme.useToken();
  return (
    <div>
      <div style={{ fontWeight: 600, fontSize: 16 }}>{alert.host}</div>
      <div style={{ color: token.colorTextSecondary, fontSize: 12, marginTop: 4 }}>
        {alert.message}
      </div>
      <div style={{ marginTop: 8 }}>
        <SeverityTag severity={alert.severity} label={alert.severity_name} />
        <StatusTag value={alert.status} />
        <div style={{ color: token.colorTextSecondary, fontSize: 12, marginTop: 4 }}>
          {formatDateTime(alert.created_at)}
        </div>
      </div>
      <Space style={{ marginTop: 8 }}>
        {getAlertActions(alert, { onAck, onResolve, onMarkFP, onCreateTicket }).map((a) => (
          <Button
            key={a.key}
            type="link"
            size="small"
            danger={a.danger}
            disabled={a.disabled}
            onClick={a.onClick}
          >
            {a.label}
          </Button>
        ))}
      </Space>
    </div>
  );
}

export default AlertCard;
