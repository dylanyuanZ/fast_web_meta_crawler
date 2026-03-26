# -*- coding: utf-8 -*-
"""全局变量定义区"""
import enum

OUTPUT_FILE = "./output.txt"
OUTPUT_CSV = "./output.csv"
KEYWORD = "小隼 FALCAM"

import re
from urllib.parse import quote
from selenium import webdriver
from selenium.webdriver.common.keys import Keys
from selenium.webdriver.common import by
import time
import csv
from enum import Enum

def write_to_csv(data):
    with open(OUTPUT_CSV, "w+") as f:
        writer = csv.writer(f)
        writer.writerows(data)

def add_herf(herf):
    return f"http://www.youtube.com{herf}"

class VideoMetaParser:
    class ParseException(Exception):
        def __init__(self, input_str=""):
            self.err = input_str

        def get_err(self):
            return f"str [{self.err}] can't be parsed"

    class Type(enum.Enum):
        TITLE = 0
        AUTHOR = 1
        VIEWED_COUNT = 2
        RELEASE_TIME = 3
        VIDEO_TIME = 4
        FROM = 5

    meta_type = ["标题", "作者", "播放次数", "发布时间", "视频时长(s)", "来源"]
    meta_infos = []

    def get_release_time_key(self, inner_list):
        time_str = inner_list[VideoMetaParser.Type.RELEASE_TIME.value]
        pattern = re.compile(r"[\d]*")
        if not (num := pattern.search(time_str)):
            return 1  # 解析不出来的放到最后面
        num_str = num.group()
        unit = time_str[len(num_str):]
        before_map = {"年前": 6, "个月前": 5, "月前": 5, "周前": 4, "天前": 3, "小时前": 2, "分钟前": 1, "分前": 1, "秒前": 0}
        return before_map[unit] * 1000 + int(num_str)

    def write_csv(self, sort_method):
        # print(self.meta_infos)
        if sort_method == VideoMetaParser.Type.RELEASE_TIME:
            sorted_metas = sorted(self.meta_infos, key=self.get_release_time_key)
        else:
            sorted_metas = sorted(self.meta_infos, key=lambda inner_list: inner_list[sort_method.value])

        # print(sorted_metas)
        with open(OUTPUT_CSV, "w+", encoding="utf8", newline="") as f:
            try:
                writer = csv.writer(f)
                writer.writerow(self.meta_type)
                writer.writerows(sorted_metas)
            except UnicodeError:
                print(f"get an unicode parse error: {UnicodeError.with_traceback()}")

    def __repr__(self):
        return str(self.elements)

    """
    示例：
    正常视频：<a id="video-title" class="yt-simple-endpoint style-scope ytd-video-renderer" title="Vol.256 Ulanzi优篮子 MT 36小隼快拆八爪鱼三脚架手机单反相机通用拍照摄影vlog阿卡快装板支架旅游冷靴拓展便携脚架" aria-label="Vol.256 Ulanzi优篮子 MT 36小隼快拆八爪鱼三脚架手机单反相机通用拍照摄影vlog阿卡快装板支架旅游冷靴拓展便携脚架 来自bear影像志 123次观看 3年前 7分钟58秒钟" href="/watch?v=8hHhZ1TBmug&amp;pp=ygUG5bCP6Zq8">
    短视频：<a id="video-title" class="yt-simple-endpoint style-scope ytd-video-renderer" title="超酷的「小鳥」！！西工大仿生「小隼」有新突破" aria-label="超酷的「小鳥」！！西工大仿生「小隼」有新突破 来自V新派 142次观看 6个月前 10秒钟 - 播放 Shorts 短视频" href="/shorts/0epUXJkWBCQ">
    直播：<a id="video-title" class="yt-simple-endpoint style-scope ytd-video-renderer" title="【＃雑談】小星梅香のうめうめマジック?【#Vtuber】39" aria-label="【＃雑談】小星梅香のうめうめマジック?【#Vtuber】39 来自小星梅香 49次观看 直播时间：17小时前 54分钟" href="/watch?v=AIMTgRd3hI0&amp;pp=ygUG5bCP6Zq8">
    """

    def __init__(self, keyword = ""):
        self.keyword = keyword
        self.url = add_herf(f"/results?search_query={quote(keyword)}")

    def request_web_page(self):
        browser = webdriver.Chrome()
        browser.get(self.url)
        end_flag = r'<yt-formatted-string id="message" class="style-scope ytd-message-renderer">无更多结果</yt-formatted-string>'
        count = 100
        while browser.page_source.find(end_flag) == -1 and count > 0:
            count -= 1
            browser.execute_script("window.scrollBy(0,3000)", "")
            time.sleep(0.2)

        self.html = browser.page_source  # 如果页面是ajax动态加载的，这个只能获取到最初的html页面

    def write_file(self):
        with open(OUTPUT_FILE, "wb+") as f:
            f.write(self.html.encode("utf8", errors="replace"))

    def read_file(self):
        with open(OUTPUT_FILE, "r", encoding="utf8") as f:
            self.html = f.read()
            # print(self.html)

    def parse(self):
        try:
            for line in self.html.splitlines():
                meta_info = self.parse_one_line(line)
                if meta_info is not None:
                    self.meta_infos.append(meta_info)
        except VideoMetaParser.ParseException as e:
            print(f"can't parse, error is [{e.get_err()}]")

    # 解析一行html信息：如果匹配，则加入到列表；否则舍弃
    def parse_one_line(self, line):
        ret = self.get_meta_str(line)
        if ret is None:
            return None
        # print(meta_str)       #for test
        title, meta_str = ret

        elements = [""] * 6
        elements[self.Type.TITLE.value] = title

        meta_str = meta_str[len(title) + 1:]

        if (pos := meta_str.find(" - 播放 Shorts 短视频")) >= 0:
            elements[self.Type.FROM.value] = "短视频"
            meta_str = meta_str[0:pos]
        elif (pos := meta_str.find("直播时间：")) >= 0:
            elements[self.Type.FROM.value] = "直播"
            meta_str = meta_str[0:pos] + meta_str[pos + 5:]
        else:
            elements[self.Type.FROM.value] = "视频"

        i = meta_str.rfind(" ")
        if i == -1:
            raise self.ParseException(line)
        elements[self.Type.VIDEO_TIME.value] = self.grap_video_time(meta_str[i + 1:])

        e = i
        i = meta_str.rfind(" ", 0, i)
        if i == -1:
            raise self.ParseException(meta_str)
        elements[self.Type.RELEASE_TIME.value] = meta_str[i + 1:e]

        e = i
        i = meta_str.rfind(" ", 0, i)
        if i == -1:
            raise self.ParseException(meta_str)
        elements[self.Type.VIEWED_COUNT.value] = self.grap_viewed_count(meta_str[i + 1:e])
        elements[self.Type.AUTHOR.value] = meta_str[2:i]
        return elements

    def get_meta_str(self, line):
        filter_pattern1 = re.compile(
            r'<a id="video-title" class="yt-simple-endpoint style-scope ytd-video-renderer" title=".*" aria-label=".*" href=".*">')
        if not (valid_line := filter_pattern1.search(line)):
            return None
        line_str = valid_line.group()
        pos1 = line_str.find(r'aria-label="') + len(r'aria-label="')
        pos2 = line_str.rfind(r'" href="')
        aria_label = line_str[pos1: pos2]
        pos1 = line_str.find(r'title="') + len(r'title="')
        pos2 = line_str.find(r'" aria-label="')
        title = line_str[pos1: pos2]
        return title, aria_label

    # xx小时 xx分钟 xx秒钟，他们是按顺序的，但是不一定都存在
    def grap_video_time(self, input_str):
        pattern = re.compile(r"[\d]*小时|[\d]*分钟|[\d]*秒钟")
        time_result = 0
        for time_info in re.finditer(pattern, input_str):
            tmp = time_info.group()
            if tmp.endswith("小时"):
                time_result += int(tmp[:-2]) * 3600
            elif tmp.endswith("分钟"):
                time_result += int(tmp[:-2]) * 60
            elif tmp.endswith("秒钟"):
                time_result += int(tmp[:-2])

        return time_result

    def grap_viewed_count(self, input_str):
        if input_str == "无人观看":
            return 0
        new_str = input_str.removesuffix("次观看")
        return int(re.sub(r"\D", '', new_str))

if __name__ == "__main__":
    parser = VideoMetaParser(KEYWORD)  # 传入搜索关键字
    parser.request_web_page()  # 请求网页，获取资源
    parser.write_file()
    # parser.read_file()  # 也可以从文件中获取资源
    parser.parse()  # 解析网页资源，提取有效信息
    parser.write_csv(sort_method=VideoMetaParser.Type.FROM)  # 输出到csv
