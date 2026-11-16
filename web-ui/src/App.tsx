import { Layout, Menu, Typography } from "antd";
import { BarChartOutlined, ThunderboltOutlined } from "@ant-design/icons";
import { Link, Route, Routes } from "react-router-dom";
import Dashboard from "./pages/Dashboard";
import Playground from "./pages/Playground";

const { Header, Content, Footer } = Layout;

export default function App() {
  return (
    <Layout style={{ minHeight: "100vh", background: "#F5F6F8" }}>
      <Header
        style={{
          display: "flex",
          alignItems: "center",
          gap: 24,
          background: "#fff",
          borderBottom: "1px solid #E3E7ED",
          paddingLeft: 24,
        }}
      >
        <Typography.Title level={4} style={{ margin: 0 }}>
          百炼免费额度
        </Typography.Title>
        <Menu
          theme="light"
          mode="horizontal"
          style={{ flex: 1, minWidth: 240, borderBottom: "none" }}
          items={[
            { key: "/", icon: <BarChartOutlined />, label: <Link to="/">额度看板</Link> },
            {
              key: "/playground",
              icon: <ThunderboltOutlined />,
              label: <Link to="/playground">模型测试台</Link>,
            },
          ]}
        />
      </Header>
      <Content
        style={{
          padding: "16px 24px 32px",
          maxWidth: 1200,
          width: "100%",
          margin: "0 auto",
        }}
      >
        <Routes>
          <Route path="/" element={<Dashboard />} />
          <Route path="/playground" element={<Playground />} />
        </Routes>
      </Content>
      <Footer
        style={{
          background: "transparent",
          fontSize: 12.5,
          color: "#98A2B3",
          padding: "0 24px 24px",
        }}
      >
        数值为抓取时点的快照；到期与超额状态由服务端按规则实时判定。数据源：SQLite（data/quota.db）。
      </Footer>
    </Layout>
  );
}
