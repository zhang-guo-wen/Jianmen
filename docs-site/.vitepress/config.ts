import { defineConfig } from 'vitepress';

export default defineConfig({
  lang: 'zh-CN',
  title: 'Jianmen 堡垒机',
  description: '开源 SSH/SFTP/数据库代理堡垒机 — 文档',
  base: '/Jianmen/',
  themeConfig: {
    nav: [
      { text: '首页', link: '/' },
      { text: '架构', link: '/architecture' },
      { text: '版本迁移', link: '/migrations' },
      { text: '指南', link: '/guides/database-protocol-compatibility' },
    ],
    sidebar: [
      {
        text: '文档',
        items: [
          { text: '首页', link: '/' },
          { text: '架构说明', link: '/architecture' },
          { text: '版本迁移机制', link: '/migrations' },
        ],
      },
      {
        text: '指南',
        items: [
          { text: '数据库协议兼容性', link: '/guides/database-protocol-compatibility' },
          { text: '托管 guacd 容器', link: '/guides/managed-guacd-container' },
          { text: '发布', link: '/guides/release' },
          { text: 'Web RDP', link: '/guides/web-rdp' },
        ],
      },
    ],
    search: { provider: 'local' },
    darkModeSwitchLabel: '主题',
    sidebarMenuLabel: '目录',
    returnToTopLabel: '返回顶部',
  },
});
