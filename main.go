package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"text/template"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/chromedp/chromedp"
)

// 交叉编译:
// CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o gofs.exe main.go  // windows
// CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -tags netgo -o gofs main.go    // linux

// 解决alpine镜像问题, udp问题, 时区问题
// RUN mkdir /lib64 && ln -s /lib/libc.musl-x86_64.so.1 /lib64/ld-linux-x86-64.so.2 && apk add -U util-linux && apk add -U tzdata && cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime  # 解决go语言程序无法在alpine执行的问题和syslog不支持udp的问题和时区问题

const maxUploadSize = 32 * (2 << 30) // 32 * 1GB
var dir, host, port string
var reqSeconds map[string]float64
var reqTimes map[string]int64

const html = `
<!DOCTYPE html>
<html lang="zh-CN">

<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>植物信息卡片</title>
  <!-- 引入 Font Awesome CSS -->
  <link rel="stylesheet" href="https://cdnjs.cloudflare.com/ajax/libs/font-awesome/6.7.2/css/all.min.css"
    crossorigin="anonymous" referrerpolicy="no-referrer" />
  <style>
    /* 淡绿清新风格 */
    body {
      font-family: Arial, sans-serif;
      margin: 20px;
      background-color: #f0f8e6;
      /* 淡绿背景 */
      color: #333;
      /* 文字颜色 */
    }

    h1 {
      color: #4caf50;
      /* 标题颜色 (浅绿) */
      text-align: center;
      /* 标题居中 */
    }

    .card-container {
      display: flex;
      flex-wrap: wrap;
      justify-content: center;
      /* 卡片水平居中 */
      gap: 20px;
      padding: 20px;
      /* 给卡片容器加一些内边距 */
    }

    .card {
      width: 300px;
      border: 1px solid #c8e6c9;
      /* 卡片边框颜色 (浅绿) */
      border-radius: 10px;
      /* 圆角 */
      overflow: hidden;
      box-shadow: 0 4px 8px rgba(0, 0, 0, 0.1);
      /* 阴影 */
      background-color: #fff;
      /* 卡片背景色 (白色) */
      transition: transform 0.3s ease;
      margin-bottom: 20px;
      position: relative;
      /*  为新增按钮定位 */
    }

    .card:hover {
      transform: translateY(-8px);
      /* 鼠标悬停时的效果 */
    }

    .card-image {
      width: 100%;
      /* 图片宽度 */
      height: 200px;
      /* 固定图片高度 */
      object-fit: contain;
      /* 保持图片比例，可能裁剪 */
      display: block;
      /* 移除图片下方多余的空白 */
			margin-top: 10px;
    }

    .card-content {
      padding: 15px;
    }

    .card-title {
      text-align: center;
      font-size: 1.4em;
      /* 标题字体更大 */
      font-weight: bold;
      margin-bottom: 10px;
      color: #4caf50;
      /* 标题颜色 (浅绿) */
      text-decoration: underline;
      text-underline-offset: 0.3em;
      cursor: pointer;
    }

    /* 关键 - 水平排列的属性 */
    .card-summary {
      display: flex;
      justify-content: space-between;
      align-items: center;
      margin-bottom: 10px;
      font-size: 0.9em;
    }
		.card-property-label {
			color: #4caf50;
			font-weight: bold;
		}

    /* 图标容器样式 */
    .icon-container {
      position: relative;
      display: inline-block;
      text-align: center;
      color: #689f38;
      /* 图标颜色 (深绿) */
    }

    .icon-container::before {
      content: attr(data-tooltip);
      position: absolute;
      bottom: -20px;
      left: 50%;
			pointer-events: none;
      transform: translateX(-50%);
      background-color: rgba(0, 0, 0, 0.8);
      color: #fff;
      padding: 5px 10px;
      border-radius: 4px;
      font-size: 0.8em;
      white-space: nowrap;
      opacity: 0;
      visibility: hidden;
      transition: opacity 0.3s ease, visibility 0.3s ease;
      z-index: 1;
    }

    .icon-container:hover::before {
      opacity: 1;
      visibility: visible;
    }


    /* Markdown 引用的样式 */
    .markdown-quote {
      font-size: 0.9em;
      color: #558b2f;
      /* 引用文本颜色 (深绿) */
      border-left: 4px solid #a5d6a7;
      /* 引用边框颜色 (更浅的绿) */
      padding-left: 10px;
      margin-top: 5px;
			margin-bottom: 5px;
      font-style: italic;
    }

    /* 属性文字样式调整 */
    .card-property,
    .card-summary div {
      font-size: 0.85em;
      /*  字号变小 */
      color: #555;
      /* 文字颜色变浅 */
    }

    .card-property strong {
      color: #333;
      /* 保持strong标签的文字颜色不变 */
    }

    /* 新增卡片样式 */
    .add-card {
      width: 300px;
      /* 与其他卡片相同 */
      height: 300px;
      /* 保证加号居中 */
      border: 2px dashed #c8e6c9;
      /* 虚线边框 */
      border-radius: 10px;
      background-color: #f0f8e6;
      display: flex;
      justify-content: center;
      align-items: center;
      font-size: 3em;
      color: #c8e6c9;
      /* 加号颜色 */
      cursor: pointer;
      /* 鼠标样式 */
      transition: border-color 0.3s ease;
    }

    .add-card:hover {
      border-color: #4caf50;
      /* 鼠标悬停时边框颜色 */
    }

    /* 删除按钮样式 */
    .delete-button {
      position: absolute;
      top: 5px;
      right: 5px;
      background-color: rgba(255, 255, 255, 0.7);
      /* 半透明白色背景 */
      border-radius: 50%;
      width: 24px;
      height: 24px;
      text-align: center;
      line-height: 24px;
      font-size: 1em;
      color:rgb(211, 205, 205);
      /* 删除按钮颜色 (红色) */
      cursor: pointer;
      z-index: 2;
      /* 确保在最前面 */
      transition: background-color 0.2s ease;
    }

    .delete-button:hover {
      background-color:rgb(206, 238, 183);
      /* 悬停时更红 */
    }

		/* 新增卡片弹窗样式 */
    .modal {
      display: none; /* 默认隐藏 */
      position: fixed;
      top: 0;
      left: 0;
      width: 100%;
      height: 100%;
      background-color: rgba(0, 0, 0, 0.7); /* 半透明黑色背景 */
      z-index: 1000; /* 确保在最上层 */
      overflow: auto; /* 允许滚动 */
    }

    .modal-content {
      background-color: #fefefe;
      margin: 15% auto;
      padding: 15px;
      border: 1px solid #888;
      width: 80%;
      max-width: 600px;
      border-radius: 10px;
      position: relative; /* 用于定位关闭按钮 */
    }

    .close {
      position: absolute;
      top: 10px;
      right: 10px;
      font-size: 28px;
      font-weight: bold;
      color: #aaa;
      cursor: pointer;
    }

    .close:hover,
    .close:focus {
      color: black;
      text-decoration: none;
      cursor: pointer;
    }

    .search-container {
      position: relative;
      width: 80%;
      /* 文本框的宽度占容器的 80% */
      margin: 20px auto;
      /* 上下边距为20px水平居中 */
      height: 35px;
      border-radius: 20px;
      border: 2px solid #c8e6c9;
      /* 圆角 */
      background-color: #f0f8e6;
      overflow: hidden;
      box-shadow: 0 2px 5px rgba(0, 0, 0, 0.1);
    }

    .search-container input[type="text"] {
      width: 100%;
      height: 100%;
      border: none;
      outline: none;
      padding: 0 40px 0 15px;
      font-size: 16px;
      box-sizing: border-box;
    }

    .search-container input[type="text"]:focus {
      border-color: #4caf50;
    }

    .search-container .search-icon {
      position: absolute;
      right: 15px;
      top: 50%;
      transform: translateY(-50%);
      font-size: 18px;
      cursor: pointer;
      color: #888;
    }

    .image-selection {
      display: flex;
      flex-wrap: wrap;
      justify-content: space-around;
      margin-bottom: 20px;
    }

    .image-option {
      width: 100px;
      height: 100px;
      margin: 10px;
      border: 2px solid #c8e6c9;
      border-radius: 8px;
      overflow: hidden;
      cursor: pointer;
      transition: border-color 0.3s ease;
      position: relative; /* For the selected indicator */
    }

    .image-option img {
      width: 100%;
      height: 100%;
      object-fit: cover;
    }

    .image-option.selected {
      border-color: #4caf50;
    }

    .image-option::after {
      content: '\f00c'; /* Font Awesome check icon */
      font-family: 'Font Awesome 6 Free';
      font-weight: 900;
      position: absolute;
      top: 50%;
      left: 50%;
      transform: translate(-50%, -50%);
      font-size: 2em;
      color: #4caf50; /* Green color */
      opacity: 0;
      transition: opacity 0.3s ease;
      z-index: 10; /* Ensure it's on top */
    }

    .image-option.selected::after {
        opacity: 1;
    }

    .plant-info {
      margin-bottom: 20px;
      text-align: left;  /* 字段信息左对齐 */
    }

    .confirm-button {
      padding: 12px 20px;
      border: none;
      border-radius: 30px;
      font-size: 1em;
      background-color: #4caf50;
      color: white;
      cursor: pointer;
      transition: background-color 0.3s ease;
      width: 100%; /* 占据整个宽度 */
    }

    .confirm-button:hover {
      background-color: #388e3c;
    }

    .label-input-group {
        display: flex;
        align-items: center; /* 垂直居中 */
        margin-bottom: 5px;
    }

    .label-input-group label {
        display: block;
        font-weight: bold;
        background-color: #4caf50;
        color: white;
        padding: 4px 4px;
				border-top-left-radius: 4px;
				border-top-right-radius: 0;
				border-bottom-left-radius: 4px;
				border-bottom-right-radius: 0;
        font-size: 0.9em;
        white-space: nowrap; /* 防止label换行 */
    }

    .label-input-group input[type="text"] {
        width: 85%;
        flex-grow: 1; /* 允许input填充剩余空间 */
        padding: 6px;
        border: 1px solid #c8e6c9;
				border-top-left-radius: 0;
				border-top-right-radius: 4px;
				border-bottom-left-radius: 0;
				border-bottom-right-radius: 4px;
        font-size: 0.9em;
        box-sizing: border-box;
    }

    .label-input-group input[type="text"]:focus {
      border-color: #4caf50;
    }

		.loading {
        justify-content: center;
        align-items: center;
				magin-bottom: 10px;
		}

		.loading span {
        color: orange;
		}

    .oneline-container {
      display: flex; /*  使用flex布局 */
      flex-wrap: wrap; /*  允许换行 */
    }

		.oneline-container .item {
      flex: 1 1 45%; /*  每个项目占据大约一半的可用宽度，并允许换行 */
      margin-right: 2%; /*  项目之间的间距 */
      box-sizing: border-box; /*  防止宽度计算错误 */
    }

		.oneline-container .item:nth-child(2n) {
      margin-right: 0; /*  偶数个item不加右边距 */
    }


    /* 响应式布局 */
    @media (max-width: 600px) {

      .card,
      .add-card {
        width: 90%;
        /* 小屏幕上卡片占90%宽度 */
      }

			.oneline-container .item {
        flex: 1 1 100%; /* 在小屏幕上每个项目占据100%的宽度，实现垂直排列 */
        margin-right: 0;
      }
    }
  </style>
</head>

<body>

  <h1>植物卡片</h1>
  <div id="response"></div>
  <!-- 搜索框 -->
  <div class="search-container">
    <input type="text" id="searchInput" class="search-input" placeholder="搜索植物 (中文/英文名)">
		<span class="search-icon" id="searchIcon" ><i class="fas fa-search"></i></span>
  </div>

  <div class="card-container" id="plant-cards">
    <!-- 卡片将在这里动态生成 -->
    <!-- 新增卡片将在此处 -->
  </div>

<!-- 弹窗 (Modal) -->
  <div id="addPlantModal" class="modal">
    <div class="modal-content">
      <span class="close">×</span>
      <h3>新增植物</h3>
      <div class="search-container">
        <input type="text" id="plantSearchInput" placeholder="搜索植物 (中文/英文名)">
        <span class="search-icon" id="plantSearchIcon" ><i class="fas fa-search"></i></span>
      </div>
			<!-- Loading 覆盖层 -->
      <div class="loading" id="plantLoading" style="display: none;">
          <i class="fa-solid fa-spinner fa-spin-pulse"></i>
      </div>
      <div id="plantInfo" class="plant-info" style="display: none;">
        <!--  编辑字段的输入框 -->
				<div class="oneline-container">
          <div class="item">
            <div class="label-input-group">
              <label for="cnname">中文</label>
              <input type="text" id="cnname" name="cnname" value="">
            </div>
          </div>
          <div class="item">
            <div class="label-input-group">
              <label for="enname">英文</label>
              <input type="text" id="enname" name="enname" value="">
            </div>
          </div>
          <div class="item">
            <div class="label-input-group">
              <label for="family_genus">科属</label>
              <input type="text" id="family_genus" name="family_genus" value="">
            </div>
          </div>
          <div class="item">
            <div class="label-input-group">
              <label for="category">类别</label>
              <input type="text" id="category" name="category" value="">
            </div>
          </div>
				</div>
				<div class="oneline-container">
          <div class="item">
            <div class="label-input-group">
              <label for="size">大小</label>
              <input type="text" id="size" name="size" value="">
            </div>
          </div>
          <div class="item">
            <div class="label-input-group">
              <label for="temperature">温度</label>
              <input type="text" id="temperature" name="temperature" value="">
            </div>
          </div>
          <div class="item">
            <div class="label-input-group">
              <label for="toxicity">毒性</label>
              <input type="text" id="toxicity" name="toxicity" value="">
            </div>
          </div>
          <div class="item">
            <div class="label-input-group">
              <label for="flowering_period">花期</label>
              <input type="text" id="flowering_period" name="flowering_period" value="">
            </div>
          </div>
				</div>
				<div class="oneline-container">
          <div class="item">
            <div class="label-input-group">
              <label for="distribution">分布</label>
              <input type="text" id="distribution" name="distribution" value="">
            </div>
          </div>
          <div class="item">
            <div class="label-input-group">
              <label for="light">光照</label>
              <input type="text" id="light" name="light" value="">
            </div>
          </div>
				</div>
				<div class="label-input-group">
          <label for="habit">习性</label>
          <input type="text" id="habit" name="habit" value="">
				</div>
				<div class="label-input-group">
          <label for="watering">浇水</label>
          <input type="text" id="watering" name="watering" value="">
				</div>
				<div class="label-input-group">
          <label for="fertilization">施肥</label>
          <input type="text" id="fertilization" name="fertilization" value="">
				</div>
				<div class="label-input-group">
          <label for="notes">备注</label>
          <input type="text" id="notes" name="notes" value="">
				</div>
				<div class="label-input-group">
          <label for="link">链接</label>
          <input type="text" id="link" name="link" value="">
				</div>
        <div id="imageSelection" class="image-selection">
          <!-- 图片选项将在这里动态生成 -->
        </div>
      </div>
      <button type="button" id="confirmAddButton" class="confirm-button" style="display: none;">确定添加</button>
    </div>
  </div>

  <script>
		//  拉取植物的函数
    async function loadPlants() {
      try {
        const response = await fetch("/load"); // 发送 GET 请求
        if (!response.ok) {
            throw new Error(response.status+":"+await response.text());
        }

        const plants = await response.json(); // 解析 JSON 响应

        console.log(plants); // 打印 JSON 数据

				// 添加植物卡片
        const cardContainer = document.getElementById('plant-cards');
        cardContainer.innerHTML = '';  // 清空之前的卡片
        plants.forEach(plant => {
          cardContainer.appendChild(createPlantCard(plant));
        });

        // 添加新增卡片
        cardContainer.appendChild(createAddCard());

      } catch (error) {
        console.error('Fetch error:', error);
      }
    }

		//  搜索植物的函数
    async function searchPlant(query) {
      try {
        const response = await fetch("/find/" + encodeURIComponent(query));  //  向后端发送搜索请求
        if (!response.ok) {
          throw new Error(response.status+":"+await response.text());
        }
        const data = await response.json();
        return data;
      } catch (error) {
        plantLoadingDiv.style.display = 'flex';
				plantLoadingDiv.innerHTML = '<span>植物搜索失败:' + error + '</span>';
      }
    }

		//  添加植物的函数
    async function addPlant(plant) {
      try {
        const response = await fetch('/add', { //  修改为 /add 路径
          method: 'POST',
          headers: {
            'Content-Type': 'application/json'
          },
          body: JSON.stringify(plant)
        });

        if (!response.ok) {
          throw new Error(response.status+":"+await response.text());
        }

        const newPlant = await response.json(); //  获取新添加的植物
				console.log(newPlant); //  打印新添加的植物
				appendPlantCard(createPlantCard(newPlant)); //  添加新植物卡片
        // renderPlantCards(plants); //  重新渲染卡片
        closeAddPlantModal(); //  关闭弹窗
      } catch (error) {
        plantLoadingDiv.style.display = 'flex';
				plantLoadingDiv.innerHTML = '<span>植物添加失败:' + error + '</span>';
      }
    }

		//  删除植物的函数
    async function deletePlant(name) {
      try {
        const response = await fetch('/del/'+name, { //  修改为 /del 路径
          method: 'DELETE',
        });

        if (!response.ok) {
          throw new Error(response.status+":"+await response.text());
        }

        const cardContainer = document.getElementById('plant-cards');
				for (let i = cardContainer.children.length - 1; i >= 0; i--) {  // 从后往前遍历，避免索引问题
          const child = cardContainer.children[i];

          // 检查是否有 data-name 属性，并且属性值为 "ling"
          if (cardContainer.children[i].dataset && (cardContainer.children[i].dataset.cnname === name || cardContainer.children[i].dataset.enname === name)) {
            // 删除该子元素
            cardContainer.removeChild(cardContainer.children[i]);
          }
        }
      } catch (error) {
        plantLoadingDiv.style.display = 'flex';
				plantLoadingDiv.innerHTML = '<span>植物删除失败:' + error + '</span>';
      }
    }

		// 根据category值获取对应的图标
    function getCategoryIcon(category) {
        if (category.includes("草本")) {
            return '<i class="fas fa-seedling"></i>'; // 苗
        } else if (category.includes("乔木")) {
            return '<i class="fas fa-tree"></i>'; // 树
        } else if (category.includes("灌木")) {
            return '<i class="fa-solid fa-plant-wilt"></i>'; // 灌木
        } else if (category.includes("藤")) {
            return '<i class="fas fa-vine"></i>'; // 藤蔓
        }else if (category.includes("木本")) {
            return '<i class="fas fa-tree"></i>'; // 灌木
        } else {
            return '<i class="fas fa-leaf"></i>'; // 默认叶子
        }
    }

		// 根据毒性等级获取对应的图标
    function getToxicityIcon(toxicityLevel) {
        switch (toxicityLevel) {
            case "无":
                return '<i class="fas fa-check-circle"></i>'; // 对勾
            case "低":
                return '<i class="fas fa-exclamation-triangle"></i>'; // 警告
            case "中":
                return '<i class="fas fa-skull"></i>'; // 骷髅头
            case "高":
                return '<i class="fas fa-skull-crossbones"></i>'; // 红色骷髅头
            default:
                return '<i class="fas fa-skull"></i>'; // 默认骷髅头
        }
    }

		// 根据喜光度获取对应的图标
    function getLightIcon(lightLevel) {
        switch (lightLevel) {
            case "全日照":
                return '<i class="fas fa-sun"></i>'; // 太阳
            case "半日照":
                return '<i class="fas fa-cloud-sun"></i>'; // 半阴
            case "无日照":
                return '<i class="fas fa-cloud"></i>'; // 阴
            default:
                return '<i class="fas fa-cloud"></i>'; // 默认阴
        }
    }

    // 创建卡片的函数
    function createPlantCard(plant) {
      const card = document.createElement('div');
      card.classList.add('card');
			card.setAttribute('data-cnname', plant.cnname);
			card.setAttribute('data-enname', plant.enname);

      card.innerHTML = ` + "`" + `
            <img src="${plant.image}" alt="${plant.cnname}" class="card-image">
            <div class="card-content">
                <div class="card-title" onclick="window.open('${plant.link}', '_blank')">${plant.cnname} (${plant.enname})</div>
                <div class="card-summary">
                    <div class="icon-container" data-tooltip="${plant.category}">${getCategoryIcon(plant.category)}</div>
                    <div>${plant.size}</div>
										<div class="icon-container" data-tooltip="${plant.light}">${getLightIcon(plant.ilight)}</div>
                    <div>${plant.temperature}</div>
                    <div class="icon-container" data-tooltip="${plant.toxicity}">${getToxicityIcon(plant.itoxicity)}</div>
                </div>
                <div class="markdown-quote">${plant.notes}</div>
                <div class="card-property"><span class="card-property-label">科属</span> ${plant.family_genus}</div>
                <div class="card-property"><span class="card-property-label">习性</span> ${plant.habit}</div>
                <div class="card-property"><span class="card-property-label">分布</span> ${plant.distribution}</div>
                <div class="card-property"><span class="card-property-label">花期</span> ${plant.flowering_period}</div>
                <div class="card-property"><span class="card-property-label">光照</span> ${plant.light}</div>
                <div class="card-property"><span class="card-property-label">浇水</span> ${plant.watering}</div>
                <div class="card-property"><span class="card-property-label">施肥</span> ${plant.fertilization}</div>
            </div>
            ` + "`" + `;

      // 创建删除按钮
      const deleteButton = document.createElement('div');
      deleteButton.classList.add('delete-button');
			deleteButton.setAttribute('data-cnname', plant.cnname);
      deleteButton.innerHTML = '<i class="fas fa-times"></i>'; // Font Awesome 叉号图标
      deleteButton.addEventListener('click', (event) => {
        event.stopPropagation(); // 阻止点击删除按钮时触发卡片的点击事件
        if (confirm("确定要删除"+plant.cnname+"吗?")) {
          //  删除植物
          deletePlant(plant.cnname);
        }
      });
      card.appendChild(deleteButton);

      return card;
    }

    // 创建新增卡片
    function createAddCard() {
      const addCard = document.createElement('div');
      addCard.classList.add('add-card');
      addCard.innerHTML = '<i class="fas fa-plus"></i>';
      addCard.addEventListener('click', () => {
        //alert('点击新增植物');  // 替换为你的新增植物逻辑
				openAddPlantModal();
      });
      return addCard;
    }

		function appendPlantCard(card) {
			const cardContainer = document.getElementById('plant-cards');
			if (cardContainer && cardContainer.lastElementChild && cardContainer.lastElementChild.classList && cardContainer.lastElementChild.classList.contains('add-card')) {
				cardContainer.removeChild(cardContainer.lastElementChild);
			}
			cardContainer.appendChild(card);

			cardContainer.appendChild(createAddCard());
		}

    //  创建所有卡片的函数，并初始化
    function renderPlantCards(plants) {
      const cardContainer = document.getElementById('plant-cards');
      cardContainer.innerHTML = '';  // 清空之前的卡片
      plants.forEach(plant => {
        cardContainer.appendChild(createPlantCard(plant));
      });

      // 添加新增卡片
      cardContainer.appendChild(createAddCard());
    }

    // 过滤函数
    function handlePlantFilter() {
      const searchTerm = document.getElementById('searchInput').value.toLowerCase();

			const cardContainer = document.getElementById('plant-cards');
			for (let i = cardContainer.children.length - 1; i >= 0; i--) {  // 从后往前遍历，避免索引问题
        const child = cardContainer.children[i];

        if (cardContainer.children[i].dataset && cardContainer.children[i].dataset.cnname && cardContainer.children[i].dataset.enname && (cardContainer.children[i].dataset.cnname.toLowerCase().includes(searchTerm) || cardContainer.children[i].dataset.enname.toLowerCase().includes(searchTerm))) {
          cardContainer.children[i].style.display = 'block';
        } else {
					cardContainer.children[i].style.display = 'none';
				}
      }
    }

    // 初始化加载所有卡片
		loadPlants();

    // 添加搜索框的事件监听器
    document.getElementById('searchInput').addEventListener('input', handlePlantFilter);
		// 按回车键
    document.getElementById('searchIcon').addEventListener('keydown', function (event) {
      if (event.key === 'Enter') {
        handlePlantFilter();
        event.preventDefault(); // 阻止表单提交(如果页面有表单)
      }
    });

		//  弹窗相关代码
    //  弹窗相关代码
    const addPlantModal = document.getElementById('addPlantModal');
    const closeButton = document.querySelector('.close');
    const plantSearchInput = document.getElementById('plantSearchInput');
    const plantSearchIcon = document.getElementById('plantSearchIcon');
		const plantLoadingDiv = document.getElementById('plantLoading');
    const plantInfoDiv = document.getElementById('plantInfo');
    const imageSelectionDiv = document.getElementById('imageSelection');
    const confirmAddButton = document.getElementById('confirmAddButton');
    //  获取所有的输入框，用于编辑植物信息
    const cnnameInput = document.getElementById('cnname');
    const ennameInput = document.getElementById('enname');
    const familyGenusInput = document.getElementById('family_genus');
    const categoryInput = document.getElementById('category');
    const habitInput = document.getElementById('habit');
    const distributionInput = document.getElementById('distribution');
    const sizeInput = document.getElementById('size');
    const toxicityInput = document.getElementById('toxicity');
    const floweringPeriodInput = document.getElementById('flowering_period');
    const lightInput = document.getElementById('light');
    const temperatureInput = document.getElementById('temperature');
    const wateringInput = document.getElementById('watering');
    const fertilizationInput = document.getElementById('fertilization');
    const notesInput = document.getElementById('notes');
    const linkInput = document.getElementById('link');

    //  打开新增植物弹窗
    function openAddPlantModal() {
      addPlantModal.style.display = 'block';
    }

    //  关闭新增植物弹窗
    function closeAddPlantModal() {
      addPlantModal.style.display = 'none';
      //  重置弹窗状态
      plantSearchInput.value = '';
			plantLoadingDiv.style.display = 'none';
      plantInfoDiv.style.display = 'none';
      imageSelectionDiv.innerHTML = '';
      confirmAddButton.style.display = 'none';
      //  清空输入框
      cnnameInput.value = '';
      ennameInput.value = '';
      familyGenusInput.value = '';
      categoryInput.value = '';
      habitInput.value = '';
      distributionInput.value = '';
      sizeInput.value = '';
      toxicityInput.value = '';
      floweringPeriodInput.value = '';
      lightInput.value = '';
      temperatureInput.value = '';
      wateringInput.value = '';
      fertilizationInput.value = '';
      notesInput.value = '';
      linkInput.value = '';
			document.querySelectorAll('.image-option').forEach(div => div.classList.remove('selected'));
    }

    // 点击关闭按钮关闭弹窗
    closeButton.addEventListener('click', closeAddPlantModal);

    //  点击弹窗外部关闭弹窗
    window.addEventListener('click', (event) => {
      if (event.target == addPlantModal) {
        closeAddPlantModal();
      }
    });

		// 点击搜索图标
    plantSearchIcon.addEventListener('click', handlePlantSearch);

    // 按回车键
    plantSearchInput.addEventListener('keydown', function (event) {
      if (event.key === 'Enter') {
        handlePlantSearch();
        event.preventDefault(); // 阻止表单提交(如果页面有表单)
      }
    });

		 //  搜索按钮的点击事件
    async function handlePlantSearch() {
			plantLoadingDiv.innerHTML = '<i class="fa-solid fa-spinner fa-spin-pulse"></i>';
			plantLoadingDiv.style.display = 'flex';
      const query = plantSearchInput.value.trim();
      if (!query) {
        loadingSpinnerSpan.innerHTML = '请输入植物名称!';
        return;
      }
      const plant = await searchPlant(query);
			console.log(plant);
      if (!plant || plant.cnname === undefined || plant.cnname === "") {
        plantLoadingDiv.innerHTML = '<span>搜索失败请手动输入</span>';
        plantInfoDiv.style.display = 'block';
        imageSelectionDiv.innerHTML = '';
        confirmAddButton.style.display = 'none';
				//  清空之前的编辑字段
        cnnameInput.value = '';
        ennameInput.value = '';
        familyGenusInput.value = '';
        categoryInput.value = '';
        habitInput.value = '';
        distributionInput.value = '';
        sizeInput.value = '';
        toxicityInput.value = '';
        floweringPeriodInput.value = '';
        lightInput.value = '';
        temperatureInput.value = '';
        wateringInput.value = '';
        fertilizationInput.value = '';
        notesInput.value = '';
				linkInput.value = '';
				document.querySelectorAll('.image-option').forEach(div => div.classList.remove('selected'));
        return;
      }

			plantLoadingDiv.style.display = 'none';

			//  将搜索到的信息填充到输入框中
      cnnameInput.value = plant.cnname;
      ennameInput.value = plant.enname;
      familyGenusInput.value = plant.family_genus || '';
      categoryInput.value = plant.category || '';
      habitInput.value = plant.habit || '';
      distributionInput.value = plant.distribution || '';
      sizeInput.value = plant.size || '';
      toxicityInput.value = plant.toxicity || '';
      floweringPeriodInput.value = plant.flowering_period || '';
      lightInput.value = plant.light || '';
      temperatureInput.value = plant.temperature || '';
      wateringInput.value = plant.watering || '';
      fertilizationInput.value = plant.fertilization || '';
      notesInput.value = plant.notes || '';
      linkInput.value = plant.link || '';

			if (plant.images && plant.images.length > 0) {
        //  显示图片选项
        imageSelectionDiv.innerHTML = '';  //  清空之前的图片
        plant.images.forEach((imageUrl, index) => {
          const imageOption = document.createElement('div');
          imageOption.classList.add('image-option');
          imageOption.innerHTML = "<img src='" + imageUrl + "' alt='" + "plant" + index + "'>";
          imageOption.addEventListener('click', () => {
					  //  移除所有已选中的类
            document.querySelectorAll('.image-option').forEach(div => div.classList.remove('selected'));
            //  给选中的div添加选中类
            imageOption.classList.add('selected');
            confirmAddButton.style.display = 'block';  //  显示确认按钮
          });
          imageSelectionDiv.appendChild(imageOption);
        });
			}

      plantInfoDiv.style.display = 'block';
    };

		//  确认添加按钮的点击事件
    confirmAddButton.addEventListener('click', () => {
      if (cnnameInput.value.trim() === '') {
        plantLoadingDiv.style.display = 'flex';
				plantLoadingDiv.innerHTML = '<span>植物中文名不能为空</span>';
        return;
      }

			let imageUrl = "";
			if (imageSelectionDiv.children && imageSelectionDiv.children.length > 0) {
        for (let i = 0; i < imageSelectionDiv.children.length; i++) {
          const child = imageSelectionDiv.children[i];
          if (child.classList && child.classList.contains('selected')) {
            imageUrl=child.querySelector("img").getAttribute("src");
          }
        }
        if (imageUrl == "") {
					plantLoadingDiv.style.display = 'flex';
					plantLoadingDiv.innerHTML = '<span>请选择一张图片作为植物封面</span>';
          return;
				}
			}

      //  构造要发送到服务端的数据
      const newPlant = {
        cnname: cnnameInput.value,
        enname: ennameInput.value,
        family_genus: familyGenusInput.value,
        category: categoryInput.value,
        habit: habitInput.value,
        distribution: distributionInput.value,
        size: sizeInput.value,
        toxicity: toxicityInput.value,
        flowering_period: floweringPeriodInput.value,
        light: lightInput.value,
        temperature: temperatureInput.value,
        watering: wateringInput.value,
        fertilization: fertilizationInput.value,
        notes: notesInput.value,
        link: linkInput.value,
        image: imageUrl,
      };

      //  发送添加请求到服务端
      addPlant(newPlant);
    });
  </script>
</body>

</html>
`

type Plant struct {
	Cnname          string   `json:"cnname"`
	Enname          string   `json:"enname"`
	FamilyGenus     string   `json:"family_genus"`
	Category        string   `json:"category"`
	Icategory       string   `json:"icategory"`
	Habit           string   `json:"habit"`
	Distribution    string   `json:"distribution"`
	Size            string   `json:"size"`
	Toxicity        string   `json:"toxicity"`
	Itoxicity       string   `json:"itoxicity"`
	FloweringPeriod string   `json:"flowering_period"`
	Light           string   `json:"light"`
	Ilight          string   `json:"ilight"`
	Temperature     string   `json:"temperature"`
	Watering        string   `json:"watering"`
	Fertilization   string   `json:"fertilization"`
	Notes           string   `json:"notes"`
	Link            string   `json:"link"`
	Image           string   `json:"image"`
	Images          []string `json:"images"`
}

var plants = []*Plant{}

func init() {
	// 1. 打开文件
	file, err := os.Open("plants.json")
	if err != nil {
		log.Println("failed to open file:", err)
		return
	}
	defer file.Close()

	// 2. 读取文件内容
	bytes, err := io.ReadAll(file)
	if err != nil {
		log.Println("failed to read file:", err)
	}

	// 3. 解析 JSON 数据到结构体
	err = json.Unmarshal(bytes, &plants)
	if err != nil {
		log.Println("failed to unmarshal json:", err)
	}
}

func contain(ps []*Plant, name string) bool {
	for _, p := range ps {
		if p.Cnname == name || p.Enname == name {
			return true
		}
	}

	return false
}

func htmlbyhttp(urlstr string) (string, error) {
	// 使用 HTTP GET 请求获取网页内容
	resp, err := http.Get(urlstr)
	if err != nil {
		log.Println("http get failed:", err)
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Println("http get status error:", resp.StatusCode)
		return "", fmt.Errorf("http get status error: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Println("read response body error:", err)
		return "", err
	}

	return string(body), nil
}

func htmlbychromedp(urlstr string, selector string) (string, error) {
	options := []chromedp.ExecAllocatorOption{
		chromedp.Flag("headless", true), // debug使用
		chromedp.Flag("blink-settings", "imagesEnabled=true"),
		chromedp.UserAgent(`Mozilla/5.0 (Windows NT 6.3; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/73.0.3683.103 Safari/537.36`),
	}
	options = append(chromedp.DefaultExecAllocatorOptions[:], options...)

	cdpCtx, _ := chromedp.NewExecAllocator(context.Background(), options...)

	chromeCtx, _ := chromedp.NewContext(cdpCtx)

	timeoutCtx, cancel := context.WithTimeout(chromeCtx, 10*time.Second)
	defer cancel()

	var htmlContent string
	err := chromedp.Run(timeoutCtx,
		chromedp.Navigate(urlstr),
		chromedp.WaitVisible(selector),
		chromedp.OuterHTML("html", &htmlContent),
	)
	if err != nil {
		log.Println("chromedp run err:", err)
		return "", err
	}

	return htmlContent, nil
}

func fetchImages(platform, selector, name string) []string {
	var images []string

	switch platform {
	case "baidu":
		docstr, _ := htmlbychromedp(fmt.Sprintf("https://image.baidu.com/search/index?word=%s", name+"盆栽"), selector)
		// 将 HTTP 响应体转换为 goquery 的 Document
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(docstr))
		if err != nil {
			log.Println("create goquery document error:", err)
			return nil
		}

		// 选择图片元素
		doc.Find(selector).Each(func(i int, s *goquery.Selection) {
			src, exists := s.Attr("src")
			if exists && len(images) < 4 {
				if strings.HasPrefix(src, "//") {
					src = fmt.Sprintf("https:%s", src)
				}
				images = append(images, src)
			}
		})
	case "iplant":
		docstr, _ := htmlbychromedp(fmt.Sprintf("https://www.iplant.cn/info/%s", name), selector)
		// 将 HTTP 响应体转换为 goquery 的 Document
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(docstr))
		if err != nil {
			log.Println("create goquery document error:", err)
			return nil
		}

		// 选择图片元素
		doc.Find(selector).Each(func(i int, s *goquery.Selection) {
			src, exists := s.Attr("src")
			if exists {
				if strings.Contains(src, "/148/") {
					if strings.HasPrefix(src, "//") {
						src = fmt.Sprintf("https:%s", src)
					}
					images = append(images, src)
				}
			}
		})
	case "garden":
		docstr, _ := htmlbychromedp(fmt.Sprintf("https://garden.org/search/index.php?q=%s", strings.ReplaceAll(name, " ", "+")), selector)
		// 将 HTTP 响应体转换为 goquery 的 Document
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(docstr))
		if err != nil {
			log.Println("create goquery document error:", err)
			return nil
		}

		// 选择图片元素
		doc.Find(selector).Each(func(i int, s *goquery.Selection) {
			src, exists := s.Attr("src")
			if exists && len(images) < 3 {
				if strings.HasPrefix(src, "//") {
					src = fmt.Sprintf("https:%s", src)
				}
				images = append(images, src)
			}
		})
	}

	return images
}

func fetchInfo(name string) []*Plant {
	var pls []*Plant
	cont, err := reqAI(name)
	if err != nil {
		log.Println("request ai error:", err)
		return nil
	}

	err = json.Unmarshal([]byte(cont), &pls)
	if err != nil {
		var pl *Plant
		err = json.Unmarshal([]byte(cont), &pl)
		if err != nil {
			log.Println("unmarshaling json error:", err)
			return nil
		}

		pls = append(pls, pl)
		for _, p := range pls {
			fmt.Println(p)
		}
	}

	for _, plant := range pls {
		plant.Images = fetchImages("baidu", "div#waterfall img", plant.Cnname)
	}

	return pls
}

func reqAI(question string) (string, error) {
	// 构建请求体
	apiKey := "AIzaSyAVi5soZI--MAKHhWGnk-Z3nctSxlqyEt4"
	requestBody := map[string]interface{}{
		"model": "gemini-2.0-flash-lite", //  根据你的需求选择模型
		"messages": []map[string]string{
			{"role": "system", "content": "你是一个资深植物专家, 我会问你几种植物, 每种植物以空格间隔, 你需要用简短的文字回答各个植物的中文(cnname), 英文(enname), 科属(family_genus), 类别(category), 习性(habit), 分布(distribution), 尺寸(size), 毒性(toxicity), 花期(flowering_period), 光照(light), 温度(temperature), 浇水(watering), 施肥(fertilization), 简介(notes), 百科(link). 回答输出的格式为json数组(数组中每一项均为单层json对象). 其中科属的格式为xx科xx属, 尺寸的格式为xx-xxcm, 温度的格式为xx-xx°C, 习性为生态喜好和忌讳, 花期明确月份, 浇水和施肥明确周期, 光照明确喜光度, 百科为其中文维基百科的链接, 类别为草本木本分类(草本明确几年生, 木本明确是乔木灌木还是藤木)"},
			{"role": "user", "content": question},
		},
	}

	// 将请求体转换为 JSON
	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request body: %w", err)
	}

	// 创建 HTTP 请求
	req, err := http.NewRequest("POST", "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions", bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// 设置请求头
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	proxyURLStr := os.Getenv("HTTP_PROXY") // 或者使用 HTTPS_PROXY，取决于你的代理设置
	if proxyURLStr == "" {
		proxyURLStr = "http://127.0.0.1:7897"
	}

	proxyURL, _ := url.Parse(proxyURLStr)

	// 发送 HTTP 请求
	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应体
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	// 检查 HTTP 状态码
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("api request failed with status code %d: %s", resp.StatusCode, string(respBody))
	}

	// 解析 JSON 响应
	var responseMap map[string]interface{} // 使用 map[string]interface{} 接收 JSON 数据
	err = json.Unmarshal(respBody, &responseMap)
	if err != nil {
		return "", fmt.Errorf("failed to unmarshal response body: %w, body: %s", err, string(respBody))
	}

	// 提取对话结果
	if choices, ok := responseMap["choices"].([]interface{}); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]interface{}); ok {
			if message, ok := choice["message"].(map[string]interface{}); ok {
				if content, ok := message["content"].(string); ok {
					content = strings.TrimPrefix(strings.TrimSpace(content), "```json")
					content = strings.TrimSuffix(strings.TrimSpace(content), "```")
					return strings.TrimSpace(content), nil
				}
			}
		}
	}

	return "", fmt.Errorf("no choices found in response: %s", string(respBody))
}

// Gzip Compression
type gzipResponseWriter struct {
	io.Writer
	http.ResponseWriter
}

func (w gzipResponseWriter) Write(b []byte) (int, error) {
	return w.Writer.Write(b)
}

func Gzip(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			handler.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(w)
		defer gz.Close()
		gzw := gzipResponseWriter{Writer: gz, ResponseWriter: w}
		handler.ServeHTTP(gzw, r)
	})
}

func index(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	t, _ := template.New("index").Parse(html)

	t.Execute(w, nil)
	return
}

func find(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	pname := strings.TrimPrefix(r.URL.Path, "/find/")
	if r.URL.Path == "/find" {
		pname = ""
	}
	if pname == "" {
		fmt.Fprintf(w, "please input plant name")
		return
	}

	pls := fetchInfo(pname)
	if len(pls) == 0 {
		fmt.Fprintf(w, "no plant found")
		return
	}

	plant := pls[0]

	if err := json.NewEncoder(w).Encode(plant); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func add(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("reading body error: %v", err), http.StatusInternalServerError)
		return
	}
	defer r.Body.Close() // 确保关闭请求体

	// 3. 解析 JSON 数据
	var plant Plant
	err = json.Unmarshal(body, &plant)
	if err != nil {
		http.Error(w, fmt.Sprintf("unmarshalling json error: %v", err), http.StatusBadRequest)
		return
	}

	if contain(plants, plant.Cnname) || contain(plants, plant.Enname) {
		http.Error(w, "plant already exists", http.StatusBadRequest)
		return
	}

	// 填充额外字段
	if strings.Contains(plant.Category, "木本") || strings.Contains(plant.Category, "藤本") || strings.Contains(plant.Category, "乔木") || strings.Contains(plant.Category, "灌木") || strings.Contains(plant.Category, "藤木") {
		plant.Icategory = "木本"
	} else {
		plant.Icategory = "草本"
	}

	// 毒性评级
	if plant.Toxicity == "" || plant.Toxicity == "无" || strings.Contains(plant.Toxicity, "无毒") {
		plant.Itoxicity = "无"
	} else if strings.Contains(plant.Toxicity, "微毒") || strings.Contains(plant.Toxicity, "轻微") {
		plant.Itoxicity = "低"
	} else if strings.Contains(plant.Toxicity, "剧毒") || strings.Contains(plant.Toxicity, "剧烈") {
		plant.Itoxicity = "高"
	} else {
		plant.Itoxicity = "中"
	}

	// 光照评级
	if strings.Contains(plant.Light, "喜阳") || strings.Contains(plant.Light, "喜光") || strings.Contains(plant.Light, "耐阳") || strings.Contains(plant.Light, "全日照") {
		plant.Ilight = "全日照"
	} else if strings.Contains(plant.Light, "半阳") || strings.Contains(plant.Light, "半阴") || strings.Contains(plant.Light, "半日照") {
		plant.Ilight = "半日照"
	} else if strings.Contains(plant.Light, "喜阴") || strings.Contains(plant.Light, "耐阴") || strings.Contains(plant.Light, "无日照") {
		plant.Ilight = "无日照"
	} else {
		plant.Ilight = "半日照"
	}

	fmt.Println(plant)
	plants = append(plants, &plant)
	if err := flush(); err != nil {
		http.Error(w, fmt.Sprintf("flushing file error: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(plant); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func del(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	pname := strings.TrimPrefix(r.URL.Path, "/del/")
	if r.URL.Path == "/del" {
		pname = ""
	}
	if pname == "" {
		fmt.Fprintf(w, "please input plant name")
		return
	}

	if !contain(plants, pname) {
		fmt.Fprintf(w, "plant %s not exist", pname)
		return
	}

	for idx, plant := range plants {
		if plant.Cnname == pname || plant.Enname == pname {
			plants = slices.Delete(plants, idx, idx+1)
			flush()
		}
	}
}

func load(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(plants); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func healthz(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "healthy")
}

func flush() error {
	data, err := json.Marshal(plants)
	if err != nil {
		return err
	}

	file, err := os.OpenFile("plants.json", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644) // O_TRUNC: 截断文件
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// 3. 将 JSON 数据写入文件
	_, err = file.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

func main() {
	// var dport = flag.String("port", "2333", "server port")
	// var dpath = flag.String("dir", "./", "server path")
	flag.StringVar(&port, "p", "2333", "server port")
	flag.StringVar(&port, "port", "2333", "server port")

	flag.Parse()

	http.HandleFunc("/", index)
	http.HandleFunc("/healthz", healthz)
	http.HandleFunc("/healthz/", healthz)

	http.HandleFunc("/load", load)
	http.HandleFunc("/load/", load)

	http.HandleFunc("/find", find)
	http.HandleFunc("/find/", find)

	http.HandleFunc("/add", add)
	http.HandleFunc("/add/", add)

	http.HandleFunc("/del", del)
	http.HandleFunc("/del/", del)

	log.Println(fmt.Sprintf("server started at: <0.0.0.0:%s>", port))
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}

}
