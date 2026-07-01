// C7: 首次登录强改密页
// 设计:
//   - 改密表单: 旧密码 + 新密码 (8+字符, 字母+数字, 跟旧密码不同)
//   - "本次跳过" 按钮: 只在 ?reason != first-login 时显示
//   - 首次登录 (?reason=first-login) 强制改, 不显示跳过 (后端也会 400 拒绝)
import { useState, useEffect } from "react";
import {
  Form,
  Input,
  Button,
  Card,
  message,
  Space,
  Typography,
  Alert,
} from "antd";
import { LockOutlined, SafetyOutlined } from "@ant-design/icons";
import { useNavigate, useSearchParams } from "react-router-dom";
import { authApi } from "../services/api";

const { Title, Text } = Typography;

function ChangePassword() {
  const [loading, setLoading] = useState(false);
  const [skipping, setSkipping] = useState(false);
  const [form] = Form.useForm();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();

  // 主人 7/02 决策: 首次登录强改密不可跳
  // reason=first_login → 隐藏"本次跳过"按钮 (后端会 400 拒绝,前端双保险)
  const reason = searchParams.get("reason") || "optional";
  const isFirstLogin = reason === "first_login";

  // 首次登录场景: 已登录, 但 must_change_password 还在 (后端没清) — 阻止回 dashboard
  // (实际是: Login.tsx 已 redirect 过来, 用户必走完改密才能离开)
  useEffect(() => {
    // 改密页无 session guard — 走 cookie 鉴权, 后端 ChangePassword 401 会触发全局登出
  }, []);

  // 客户端强度校验 (后端 validatePasswordStrength 也校验, 这里先挡一道)
  const validateNewPassword = (_: any, value: string) => {
    if (!value) return Promise.reject(new Error("请输入新密码"));
    if (value.length < 8) return Promise.reject(new Error("密码至少 8 个字符"));
    if (!/[a-zA-Z]/.test(value) || !/[0-9]/.test(value)) {
      return Promise.reject(new Error("密码必须同时包含字母和数字"));
    }
    return Promise.resolve();
  };

  const onFinish = async (values: {
    old_password: string;
    new_password: string;
  }) => {
    setLoading(true);
    try {
      await authApi.changePassword(values.old_password, values.new_password);
      message.success("密码修改成功");
      // 改完密回 dashboard
      navigate("/", { replace: true });
    } catch (error: any) {
      message.error(error.response?.data?.message || "密码修改失败");
    } finally {
      setLoading(false);
    }
  };

  // 跳过 (非首次场景 only) — reason=optional 或不带参数
  // 调 skipPasswordChange API 写 audit + 清 flag
  const handleSkip = async () => {
    setSkipping(true);
    try {
      await authApi.skipPasswordChange("optional");
      message.success("已跳过改密");
      navigate("/", { replace: true });
    } catch (error: any) {
      message.error(error.response?.data?.message || "跳过失败");
    } finally {
      setSkipping(false);
    }
  };

  return (
    <div
      style={{
        minHeight: "100vh",
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        background:
          "linear-gradient(135deg, #667eea 0%, #764ba2 50%, #f093fb 100%)",
        padding: 16,
      }}
    >
      <Card
        style={{
          width: 460,
          boxShadow: "0 20px 50px rgba(0, 0, 0, 0.25)",
          borderRadius: 12,
          border: "none",
        }}
        styles={{ body: { padding: "40px 36px 32px" } }}
      >
        {/* Logo + 标题 */}
        <div style={{ textAlign: "center", marginBottom: 24 }}>
          <div
            style={{
              width: 64,
              height: 64,
              margin: "0 auto 16px",
              borderRadius: 16,
              background: "linear-gradient(135deg, #667eea 0%, #764ba2 100%)",
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
              boxShadow: "0 8px 16px rgba(102, 126, 234, 0.4)",
            }}
          >
            <SafetyOutlined style={{ fontSize: 32, color: "#fff" }} />
          </div>
          <Title level={3} style={{ margin: 0, marginBottom: 4 }}>
            修改密码
          </Title>
          <Text type="secondary" style={{ fontSize: 13 }}>
            {isFirstLogin ? "首次登录请修改默认密码" : "修改您的登录密码"}
          </Text>
        </div>

        {isFirstLogin && (
          <Alert
            type="warning"
            showIcon
            message="首次登录必须修改默认密码"
            description="为安全起见, 首次登录不能跳过改密步骤。修改完成后即可正常使用系统。"
            style={{ marginBottom: 20 }}
          />
        )}

        <Form
          form={form}
          name="change-password"
          onFinish={onFinish}
          autoComplete="off"
          size="large"
        >
          <Form.Item
            name="old_password"
            rules={[{ required: true, message: "请输入旧密码" }]}
          >
            <Input.Password
              prefix={<LockOutlined style={{ color: "rgba(0,0,0,0.45)" }} />}
              placeholder="旧密码"
              autoComplete="current-password"
            />
          </Form.Item>

          <Form.Item
            name="new_password"
            rules={[{ required: true, validator: validateNewPassword }]}
            hasFeedback
          >
            <Input.Password
              prefix={<LockOutlined style={{ color: "rgba(0,0,0,0.45)" }} />}
              placeholder="新密码 (至少 8 位, 含字母和数字)"
              autoComplete="new-password"
            />
          </Form.Item>

          <Form.Item
            name="confirm_password"
            dependencies={["new_password"]}
            rules={[
              { required: true, message: "请确认新密码" },
              ({ getFieldValue }) => ({
                validator(_, value) {
                  if (!value || getFieldValue("new_password") === value) {
                    return Promise.resolve();
                  }
                  return Promise.reject(new Error("两次输入的密码不一致"));
                },
              }),
            ]}
            hasFeedback
          >
            <Input.Password
              prefix={<LockOutlined style={{ color: "rgba(0,0,0,0.45)" }} />}
              placeholder="确认新密码"
              autoComplete="new-password"
            />
          </Form.Item>

          <Form.Item style={{ marginBottom: 12 }}>
            <Space.Compact style={{ width: "100%" }}>
              <Button
                type="primary"
                htmlType="submit"
                loading={loading}
                style={{ height: 44, fontSize: 15, fontWeight: 500, flex: 1 }}
              >
                {isFirstLogin ? "修改密码并继续" : "确认修改"}
              </Button>
              {/* 主人 7/02 决策: 首次登录不可跳 — 隐藏"本次跳过"按钮 */}
              {!isFirstLogin && (
                <Button
                  onClick={handleSkip}
                  loading={skipping}
                  style={{ height: 44, fontSize: 15 }}
                >
                  本次跳过
                </Button>
              )}
            </Space.Compact>
          </Form.Item>
        </Form>

        <div
          style={{
            textAlign: "center",
            color: "rgba(0, 0, 0, 0.45)",
            fontSize: 12,
            padding: "8px 0",
            marginTop: 8,
          }}
        >
          密码要求: 至少 8 位字符, 同时包含字母和数字
        </div>
      </Card>
    </div>
  );
}

export default ChangePassword;
