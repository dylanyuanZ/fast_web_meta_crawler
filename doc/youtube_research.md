# 预调研
## 视频搜索页面探究
### 冷门搜索
我们就以[dfgvhjisfdhg](https://www.youtube.com/results?search_query=dfgvhjisfdhg)这个超级冷门的搜索关键字为例，得到的界面如下：
<img width="3840" height="1907" alt="Image" src="https://github.com/user-attachments/assets/932eec0c-909e-4a70-ac45-89778a0f0d6e" />
然后看一下筛选能力，本以为筛选结果会符合预期，但当我真的用这个筛选功能却发现，根本筛不出来东西。我猜测dfgvhjisfdhg的这些内容可能是youtube因为根据关键字根本查不出来东西，强行塞到页面上比较好看的；但筛选必须要查数据库，从数据库里是啥也查不到。
<img width="1040" height="781" alt="Image" src="https://github.com/user-attachments/assets/af5b7a61-8461-4c49-bb6c-6399a1601302" />
<img width="1822" height="1017" alt="Image" src="https://github.com/user-attachments/assets/32dbb26b-ddf1-4897-945c-5cfea5cebeaa" />
但通过这次搜索，我们知道了youtube对于数据展示是有极限的，实在没数据就会显示"无更多结果"。这一点比bilibili强，bilibili不管搜啥都是展示一堆视频数据，可能绝大部分视频都和搜索内容一点关系都没有。
<img width="2138" height="583" alt="Image" src="https://github.com/user-attachments/assets/453bfa75-347b-485a-bf16-303390f79e1f" />
另外如果筛选了短视频，就不能再筛选视频时长了。
<img width="1025" height="808" alt="Image" src="https://github.com/user-attachments/assets/50125d45-7a51-4e87-8e21-90f6ea05c71f" />

### 一般搜索
那么我们再用一般数据[人福药业](https://www.youtube.com/results?search_query=%E4%BA%BA%E7%A6%8F%E8%8D%AF%E4%B8%9A)搜索下看看：
1. 虽然视频也挺多的，估计有几百条，但是最终还是有边界
2. 且过滤功能能用了，说明是真的搜出来的数据
3. 说实话不管怎么筛选也有些莫名其妙的东西，感觉youtube的数据源被污染的挺严重。但也有可能只是抓住了药这个关键词。感觉如果准备做stage0的二次筛选的话，可以用这个搜索词来测试
<img width="2159" height="1082" alt="Image" src="https://github.com/user-attachments/assets/b3577dec-159a-45ee-917a-5afc0821dd91" />
4. 选3-20分钟的筛选，结果就几条，很适合用来测试

### 热门搜索
搜[bruno mars](https://www.youtube.com/results?search_query=bruno+mars)，即使筛选3-20分钟 + 今天发布，都有好多数据！且关联度很高

## 作者页面探究
### 首页
<img width="1365" height="330" alt="Image" src="https://github.com/user-attachments/assets/d3f9d7b3-53ce-4604-a9d6-acddd515f560" />
点击"更多"之后：
<img width="824" height="1366" alt="Image" src="https://github.com/user-attachments/assets/23a36fc6-eb58-47ac-ab91-274694296793" />
<img width="790" height="1127" alt="Image" src="https://github.com/user-attachments/assets/bb56b0d2-9725-4d46-b396-268a94051c70" />
<img width="819" height="1264" alt="Image" src="https://github.com/user-attachments/assets/6a88b68e-8b22-4df9-a409-6360b6cb0135" />
<img width="637" height="652" alt="Image" src="https://github.com/user-attachments/assets/c66bf32d-9ade-409a-a7c3-cb39ead0b692" />

可以看到，首页里最全的作者信息都集中在"更多"里面，字段包括：姓名、介绍、其他平台的链接、youtube主页链接、注册时间、粉丝数、总视频数、播放数量。但是有的youtube 博主还有"地区"字段，另外其他平台链接并不是强制的

### 视频
首页上一堆各种分类的视频堆叠在一起，还有各种合集，不利于我们统计。其实youtube有个显著特征，他会对博主的资源进行分类，包括搜索页面也是。我们目前只关心视频 or 短视频(shorts)，那么无论是在主页，还是在博主页面，都可以通过点击这两个标签来进入到对应的界面中。
视频和SHORTS能展示的信息也是不一样的：
- 视频
1. 往下拉到页面末尾则表示没有更多视频了，不过似乎没有拉倒底的专用提示。
2. 有最新、最热门、最早的排序能力
3. 视频信息包括视频名、播放数、发布时间、视频时长
<img width="2164" height="855" alt="Image" src="https://github.com/user-attachments/assets/072c82ab-2c84-4b63-a7db-b7a54bca0a81" />

- SHORTS
1. 往下拉到页面末尾则表示没有更多视频了，不过似乎没有拉倒底的专用提示。
2. 有最新、最热门、最早的排序能力
3. 视频信息包括视频名、播放数
<img width="2037" height="1061" alt="Image" src="https://github.com/user-attachments/assets/ce78e9e5-fb84-4f22-ac70-5dd3dfab3e4e" />


点进视频里面还能获得点赞数和评论数，但感觉很不划算，就这么点信息。如果要分析评论还行，如果不分析评论感觉真没啥必要。



## 结论
有了bilibili爬虫的开发经验，我知道先人为确认以下内容是很有帮助的：
1. 要做哪些阶段？在每个阶段能抓取到哪些数据？
- youtube做stage0 + stage1就够了，stage0能获取到视频和SHORTS的视频名、播放量、发布时间、作者名、视频说明
2. 是否有筛选、排序功能？有哪些有价值的筛选/排序能力？
- stage0有价值的筛选项：类型(视频/SHORTS)，视频时长(3分钟/3-20分钟/20分钟以上)，上传日期(今天/本周/本月/今年)
- stage1有价值的筛选项：类型(视频/SHORTS)
- stage0有价值的排序规则：无
- stage1有价值的排序规则：最新/最热门/最早
3. 在每个阶段要怎么抓到数据？是否可以并行处理？
- stage0：搜索关键字；根据配置进行筛选；然后往下滚动直到末尾，末尾会显示"无更多结果"
- stage1：先点击视频分类；然后点击排序规则；然后往下滚动直到末尾，末尾无多余显示，似乎得用别的方法判断结尾
4. 对于冷门、热门数据如何展示？
超级冷门数据，筛选不出结果反而是好事，总比乱返回结果好
一般和热门数据也就是正常显示内容，youtube比bilibili好的地方在于，如果真的没啥数据，它不会显示24页之多的结果的，最多也就几十条。


# 配置重构
目前的配置：
1. 比如并发度这些，全是全局的。应该分阶段
2. 比如cookie这些，全是全局的。应该分平台


# 加上时间筛选
一次性抓到所有数据，其实没必要。可以加上一个时间筛选字段，来筛选比如3年内的视频。如果平台有这个能力就最好；如果没有，就自己根据排序手动去筛；如果连排序都没有，那就只能遍历+内存里做筛选了。