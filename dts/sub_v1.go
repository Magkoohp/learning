package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/Shopify/sarama"
	cluster "github.com/bsm/sarama-cluster"
	"github.com/linkedin/goavro"

	//"alibaba-dts-go/avro"
	"github.com/LioRoger/dtsavro"
)

var (
	r io.Reader

	// 仅需改动以下配置即可
	// ***********************************************************
	kafkaUser       = "tsewell"
	kafkaPassWord   = "f4VvNGQTuEshh6RX"
	kafkaTopic      = "cn_hangzhou_rm_bp1z338e1d86alz4o_tsewell"
	kafkaGroupId    = "dtsnj211rxz19mob23"
	kafkaBrokerList = []string{"dts-cn-hangzhou.aliyuncs.com:18001"}
	// ***********************************************************
)

func main() {
	config := cluster.NewConfig()
	config.Consumer.Return.Errors = true
	config.Group.Return.Notifications = true
	config.Net.MaxOpenRequests = 100
	config.Consumer.Offsets.CommitInterval = 1 * time.Second
	config.Consumer.Offsets.Initial = sarama.OffsetNewest
	//config.Consumer.Offsets.Initial = 1605855819
	config.Net.SASL.Enable = true
	config.Net.SASL.User = kafkaUser + "-" + kafkaGroupId
	config.Net.SASL.Password = kafkaPassWord
	config.Version = sarama.V0_11_0_0

	consumer, err := cluster.NewConsumer(kafkaBrokerList, kafkaGroupId, []string{kafkaTopic}, config)
	if err != nil {
		panic(err)
	}
	defer consumer.Close()
	// trap SIGINT to trigger a shutdown.
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)

	// consume errors
	go func() {
		for err := range consumer.Errors() {
			panic(err)
		}
	}()

	// consume notifications
	go func() {
		for ntf := range consumer.Notifications() {
			fmt.Println("Rebalanced: %+v\n", ntf)
		}
	}()

	// consume messages, watch signals
	for {
		select {
		case msg, ok := <-consumer.Messages():
			if ok {
				r = bytes.NewReader(msg.Value)

				_, err := dtsavro.DeserializeRecord(r)
				if err != nil {
					log.Fatal(err)
				}

				t := dtsavro.NewRecord()
				codec, err := goavro.NewCodec(t.Schema())
				if err != nil {
					log.Fatal(err)
				}
				native, _, err := codec.NativeFromBinary(msg.Value)
				if err != nil {
					log.Fatal("error:", err)
				}
				a := native.(map[string]interface{})
				if _, ok := a["operation"]; ok {
					if a["operation"].(string) != "HEARTBEAT" {
						texual, err := codec.TextualFromNative(nil, native)
						if err != nil {
							log.Fatal(err)
						}
						fmt.Println("texual:", string(texual))
					}
				}

				//texual, err := codec.TextualFromNative(nil, native)
				//if err != nil {
				//	log.Fatal(err)
				//}
				//fmt.Println("texual:", string(texual))
			}
		case <-signals:
			return
		}
	}
}
